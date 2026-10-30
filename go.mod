module meme-hub

go 1.26.0

require github.com/lib/pq v1.10.9

require github.com/joho/godotenv v1.5.1

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/rabbitmq/amqp091-go v1.16.0
	github.com/redis/go-redis/v9 v9.23.0
	golang.org/x/net v0.62.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	golang.org/x/oauth2 v0.27.0
)
