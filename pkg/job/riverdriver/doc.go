// Package riverdriver implements the job.Driver interface using River
// (https://riverqueue.com) with PostgreSQL as the backing store.
//
// This is the default driver for production deployments that use PostgreSQL.
// It supports transactional job enqueueing via pgx.Tx, cron-based periodic
// jobs, multiple named queues, automatic retries, and deduplication.
//
// # Usage
//
//	import (
//	    "github.com/dmitrymomot/forge/pkg/job"
//	    "github.com/dmitrymomot/forge/pkg/job/riverdriver"
//	)
//
//	pool, _ := pgxpool.New(ctx, databaseURL)
//	driver := riverdriver.New(pool)
//
//	app := forge.New(cfg,
//	    forge.WithJobs(driver, job.Config{MaxWorkers: 50},
//	        job.WithTask(tasks.NewSendWelcome(mailer)),
//	    ),
//	)
package riverdriver
