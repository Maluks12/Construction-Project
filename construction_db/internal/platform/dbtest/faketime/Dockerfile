# Postgres whose clock runs at an offset (dbtest.StartAt): tests near SAST
# midnight. Never used in production.
FROM postgres:16-alpine
RUN apk add --no-cache libfaketime
