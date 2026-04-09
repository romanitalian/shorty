package store

import "errors"

// ErrCodeCollision indicates the generated short code already exists in DynamoDB.
var ErrCodeCollision = errors.New("short code already exists")

// ErrLinkNotFound indicates the link does not exist or is not owned by the caller.
var ErrLinkNotFound = errors.New("link not found")

// ErrLinkInactiveOrLimitReached indicates the link is deactivated or has
// reached its max_clicks limit.
var ErrLinkInactiveOrLimitReached = errors.New("link inactive or click limit reached")

// ErrUserNotFound indicates the user does not exist.
var ErrUserNotFound = errors.New("user not found")
