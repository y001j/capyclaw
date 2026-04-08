package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"CapyClaw/internal/shared/config"
)

// RateLimit returns middleware that enforces per-tenant rate limiting using Redis.
func RateLimit(cfg config.RapidsConfig, redisClient *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled || redisClient == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Extract tenant and user from context
			tenantID, _ := r.Context().Value(TenantIDKey).(string)
			userID, _ := r.Context().Value(UserIDKey).(string)
			if tenantID == "" || userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			// Check per-minute limit
			minuteKey := fmt.Sprintf("ratelimit:%s:%s:min", tenantID, userID)
			count, err := redisClient.Incr(ctx, minuteKey).Result()
			if err != nil {
				// On Redis error, allow the request through (fail open)
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				redisClient.Expire(ctx, minuteKey, time.Minute)
			}

			if count > int64(cfg.DefaultLimits.RequestsPerMinute) {
				ttl, _ := redisClient.TTL(ctx, minuteKey).Result()
				w.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())+1))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.DefaultLimits.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, `{"error":{"code":"rate_limited","message":"rate limit exceeded"}}`, http.StatusTooManyRequests)
				return
			}

			// Check per-hour limit
			hourKey := fmt.Sprintf("ratelimit:%s:%s:hr", tenantID, userID)
			hourCount, err := redisClient.Incr(ctx, hourKey).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if hourCount == 1 {
				redisClient.Expire(ctx, hourKey, time.Hour)
			}

			if hourCount > int64(cfg.DefaultLimits.RequestsPerHour) {
				ttl, _ := redisClient.TTL(ctx, hourKey).Result()
				w.Header().Set("Retry-After", strconv.Itoa(int(ttl.Seconds())+1))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.DefaultLimits.RequestsPerHour))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, `{"error":{"code":"rate_limited","message":"hourly rate limit exceeded"}}`, http.StatusTooManyRequests)
				return
			}

			// Set rate limit headers
			remaining := int64(cfg.DefaultLimits.RequestsPerMinute) - count
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.DefaultLimits.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			next.ServeHTTP(w, r)
		})
	}
}
