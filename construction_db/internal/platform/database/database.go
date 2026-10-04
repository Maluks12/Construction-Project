package database

import (
	"context"
	"fmt"
	"time"

	//"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}


	if err := pool.Ping(pingCtx); err != nil{
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}