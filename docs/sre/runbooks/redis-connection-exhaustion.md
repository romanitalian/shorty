# Runbook: Redis Connection Exhaustion

**Alert:** `RedisHighConnectionCount` / `RedisDown` / `RedisHighMemoryUsage`
**Severity:** SEV2 (connections exhausted or Redis down) / SEV3 (connection count approaching limit)
**SLO Impact:** Redis unavailability forces fallback to DynamoDB, increasing redirect p99 latency well beyond the 100ms target. Cache hit ratio drop directly impacts availability SLO.

---

## Symptoms

- `RedisHighConnectionCount` alert: connections > 4000 (80% of pool limit).
- `RedisDown` alert: Redis instance not responding.
- `RedisHighMemoryUsage` alert: memory usage > 85%.
- `RedirectCacheHitRateLow` alert: cache hit ratio < 80%.
- Redirect latency spike (p99 > 100ms) as requests fall through to DynamoDB.
- Connection timeout errors in Lambda logs: `dial tcp: i/o timeout` or `ERR max number of clients reached`.

---

## Diagnosis

### 1. Check Redis connection count and client info

```bash
# Total connected clients
redis-cli -h {redis_endpoint} INFO clients

# Detailed client list (shows IP, age, idle time, command)
redis-cli -h {redis_endpoint} CLIENT LIST

# Count clients by origin
redis-cli -h {redis_endpoint} CLIENT LIST | awk -F'[ =]' '{print $4}' | sort | uniq -c | sort -rn | head -20
```

### 2. Check Redis server health

```bash
# Server info overview
redis-cli -h {redis_endpoint} INFO server

# Memory usage
redis-cli -h {redis_endpoint} INFO memory

# Check maxclients setting
redis-cli -h {redis_endpoint} CONFIG GET maxclients
```

### 3. Check slow log for blocked operations

```bash
# Last 20 slow queries (default threshold: 10ms)
redis-cli -h {redis_endpoint} SLOWLOG GET 20

# Reset slow log after review
# redis-cli -h {redis_endpoint} SLOWLOG RESET
```

### 4. Check ElastiCache metrics (production)

```bash
# Connection count
aws cloudwatch get-metric-statistics \
  --namespace AWS/ElastiCache \
  --metric-name CurrConnections \
  --dimensions Name=CacheClusterId,Value=shorty-redis \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Maximum

# Engine CPU utilization
aws cloudwatch get-metric-statistics \
  --namespace AWS/ElastiCache \
  --metric-name EngineCPUUtilization \
  --dimensions Name=CacheClusterId,Value=shorty-redis \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Maximum

# Evictions (indicates memory pressure)
aws cloudwatch get-metric-statistics \
  --namespace AWS/ElastiCache \
  --metric-name Evictions \
  --dimensions Name=CacheClusterId,Value=shorty-redis \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

### 5. Detect connection leaks

```bash
# Look for Lambda execution environments holding idle connections
# Each Lambda execution environment maintains a pool of 5 connections
# With 1000 concurrent Lambdas: 5000 connections expected

# Check long-idle connections (idle > 300s may indicate leaked connections)
redis-cli -h {redis_endpoint} CLIENT LIST | awk -F'[ =]' '{for(i=1;i<=NF;i++) if($i=="idle") print $(i+1)}' | sort -n | tail -20

# Check Lambda concurrent executions to estimate expected connections
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name ConcurrentExecutions \
  --dimensions Name=FunctionName,Value=shorty-redirect \
  --start-time "$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Maximum
```

### 6. Check Lambda logs for connection errors

```sql
-- CloudWatch Insights
filter @message like /redis/ and (@message like /timeout/ or @message like /connection refused/ or @message like /max number of clients/)
| fields @timestamp, @message, @logStream
| sort @timestamp desc
| limit 50
```

---

## Mitigation

### Step 1: Kill idle connections (immediate relief)

```bash
# Kill clients idle for more than 5 minutes (300 seconds)
redis-cli -h {redis_endpoint} CLIENT NO-EVICT ON
redis-cli -h {redis_endpoint} CONFIG SET timeout 300
```

Note: Setting a `timeout` will auto-close idle connections. This is safe because Lambda connection pools will reconnect on next use.

### Step 2: Recycle Lambda execution environments (connection leak fix)

If a connection leak is suspected, force Lambda to recycle execution environments:

```bash
# Update the description to force a new deployment (recycles all environments)
aws lambda update-function-configuration \
  --function-name shorty-redirect \
  --description "recycle $(date +%s)"
```

Warning: This causes a brief period of cold starts. Only do this during off-peak or if connections are critically exhausted.

### Step 3: Scale to a larger ElastiCache node (if maxclients is the bottleneck)

```bash
# Check current node type
aws elasticache describe-cache-clusters \
  --cache-cluster-id shorty-redis \
  --query 'CacheClusters[0].CacheNodeType'

# Modify to a larger node type (causes brief downtime during failover)
aws elasticache modify-cache-cluster \
  --cache-cluster-id shorty-redis \
  --cache-node-type cache.r7g.large \
  --apply-immediately
```

### Step 4: If Redis is completely down

The redirect Lambda is designed to fall back to DynamoDB when Redis is unavailable. Verify the fallback is working:

```bash
# Check redirect errors -- if fallback works, errors should not spike
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Errors \
  --dimensions Name=FunctionName,Value=shorty-redirect \
  --start-time "$(date -u -d '15 minutes ago' +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 60 --statistics Sum
```

If DynamoDB fallback is functional, the impact is latency degradation (SEV3) rather than outage (SEV1). Monitor until Redis recovers.

### Step 5: Flush Redis (last resort -- corrupt cache)

```bash
# CAUTION: This causes a temporary latency spike as cache rebuilds
redis-cli -h {redis_endpoint} FLUSHDB
```

---

## Prevention

1. **Connection pool sizing:** Each Lambda execution environment uses `PoolSize: 5`. Monitor `ConcurrentExecutions * PoolSize` and ensure it stays below 80% of `maxclients`.
2. **Connection timeout:** Set `timeout 300` in Redis config to auto-close idle connections from recycled Lambda environments.
3. **ElastiCache node sizing:** Choose a node type with `maxclients` headroom. `cache.r7g.large` supports ~65,000 connections.
4. **Connection monitoring:** Alert at 80% of `maxclients` (current threshold: 4000 connections).
5. **Memory management:** Set `maxmemory-policy allkeys-lru` to evict least-recently-used keys when memory is full, rather than returning errors.
6. **Provisioned concurrency cap:** The redirect Lambda has `provisioned_concurrency = 2`. If this increases, recalculate the expected connection count.

---

## Escalation

| Condition | Action |
|-----------|--------|
| Redis completely unreachable | Verify DynamoDB fallback is working; if not, follow [high-error-rate.md](high-error-rate.md) |
| Connections exhausted after recycling Lambdas | Engineering Lead -- investigate potential pool leak in application code |
| ElastiCache node scaling needed | SRE team -- plan maintenance window for node type change |
| Sustained cache miss rate > 50% | Engineering review of cache TTL and eviction policy |
