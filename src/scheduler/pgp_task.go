package scheduler

import (
	"context"
	"log"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/pgp"
)

func keyserverRetryTask() error {
	if err := pgp.RetryDue(context.Background(), config.Get()); err != nil {
		log.Printf("Scheduler: PGP keyserver retry failed: %v", err)
		return err
	}
	return nil
}
