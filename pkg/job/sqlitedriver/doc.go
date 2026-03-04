// Package sqlitedriver implements the job.Driver interface using SQLite
// as the backing store. It provides a polling-based job queue suitable for
// solo deployments that don't need PostgreSQL.
//
// The driver supports multiple named queues, automatic retries, job deduplication,
// priority ordering, and cron-based periodic jobs. All state is stored in a single
// forge_jobs table managed by the driver's Migrate method.
//
// # Usage
//
//	import (
//	    "github.com/dmitrymomot/forge/pkg/job"
//	    "github.com/dmitrymomot/forge/pkg/job/sqlitedriver"
//	    "github.com/dmitrymomot/forge/pkg/sqlitedb"
//	)
//
//	db, _ := sqlitedb.Connect("app.db")
//	driver := sqlitedriver.New(db,
//	    sqlitedriver.WithPollInterval(500 * time.Millisecond),
//	)
//
//	app := forge.New(cfg,
//	    forge.WithJobs(driver, job.Config{MaxWorkers: 10},
//	        job.WithTask(tasks.NewSendWelcome(mailer)),
//	    ),
//	)
package sqlitedriver
