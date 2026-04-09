package store

// Link represents a shortened URL record in the DynamoDB links table.
// Key pattern: PK = "LINK#{code}", SK = "META"
type Link struct {
	PK           string `dynamodbav:"PK"`
	SK           string `dynamodbav:"SK"`
	OwnerID      string `dynamodbav:"owner_id"`
	OriginalURL  string `dynamodbav:"original_url"`
	Code         string `dynamodbav:"code"`
	Title        string `dynamodbav:"title,omitempty"`
	PasswordHash string `dynamodbav:"password_hash,omitempty"`
	ExpiresAt    *int64 `dynamodbav:"expires_at,omitempty"`
	MaxClicks    *int64 `dynamodbav:"max_clicks,omitempty"`
	ClickCount   int64  `dynamodbav:"click_count"`
	IsActive     bool   `dynamodbav:"is_active"`
	UTMSource    string `dynamodbav:"utm_source,omitempty"`
	UTMMedium    string `dynamodbav:"utm_medium,omitempty"`
	UTMCampaign  string `dynamodbav:"utm_campaign,omitempty"`
	CreatedAt    int64  `dynamodbav:"created_at"`
	UpdatedAt    int64  `dynamodbav:"updated_at"`
}

// ClickEvent represents a single click record in the DynamoDB clicks table.
// Key pattern: PK = "LINK#{code}", SK = "CLICK#{unix_ts}#{uuid}"
type ClickEvent struct {
	PK            string `dynamodbav:"PK"`
	SK            string `dynamodbav:"SK"`
	IPHash        string `dynamodbav:"ip_hash"`
	Country       string `dynamodbav:"country"`
	DeviceType    string `dynamodbav:"device_type"`
	RefererDomain string `dynamodbav:"referer_domain,omitempty"`
	UserAgentHash string `dynamodbav:"user_agent_hash"`
	CreatedAt     int64  `dynamodbav:"created_at"`
	TTL           int64  `dynamodbav:"ttl"`
}

// LinkStats contains aggregate click statistics for a link.
type LinkStats struct {
	TotalClicks  int64
	UniqueClicks int64  // unique ip_hashes
	LastClickAt  *int64
}

// TimelineBucket represents click counts grouped by time period.
type TimelineBucket struct {
	Timestamp int64
	Clicks    int64
}

// GeoStat represents click counts grouped by country.
type GeoStat struct {
	Country string
	Clicks  int64
}

// ReferrerStat represents click counts grouped by referrer domain.
type ReferrerStat struct {
	Domain string
	Clicks int64
}

// User represents a user profile record in the DynamoDB users table.
// Key pattern: PK = "USER#{cognito_sub}", SK = "PROFILE"
type User struct {
	PK               string `dynamodbav:"PK"`
	SK               string `dynamodbav:"SK"`
	Email            string `dynamodbav:"email"`
	DisplayName      string `dynamodbav:"display_name,omitempty"`
	Plan             string `dynamodbav:"plan"`
	DailyLinkQuota   int64  `dynamodbav:"daily_link_quota"`
	TotalLinkQuota   int64  `dynamodbav:"total_link_quota"`
	LinksCreatedToday int64  `dynamodbav:"links_created_today"`
	TotalActiveLinks int64  `dynamodbav:"total_active_links"`
	LastResetDate    string `dynamodbav:"last_reset_date"`
	CreatedAt        int64  `dynamodbav:"created_at"`
}
