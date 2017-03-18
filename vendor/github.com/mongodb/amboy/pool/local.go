/*
Local Workers Pool

The LocalWorkers is a simple worker pool implementation that spawns a
collection of (n) workers and dispatches jobs to worker threads, that
consume work items from the Queue's Next() method.
*/
package pool

import (
	"errors"

	"github.com/mongodb/amboy"
	"github.com/mongodb/grip"
	"golang.org/x/net/context"
)

// LocalWorkers is a very minimal implementation of a worker pool, and
// supports a configurable number of workers to process Job tasks.
type LocalWorkers struct {
	size     int
	started  bool
	queue    amboy.Queue
	canceler context.CancelFunc
}

// NewLocalWorkers is a constructor for LocalWorkers objects, and
// takes arguments for the number of worker processes and a amboy.Queue
// object.
func NewLocalWorkers(numWorkers int, q amboy.Queue) *LocalWorkers {
	r := &LocalWorkers{
		queue: q,
		size:  numWorkers,
	}

	if r.size <= 0 {
		grip.Infof("setting minimal pool size is 1, overriding setting of '%d'", r.size)
		r.size = 1
	}

	return r
}

// SetQueue allows callers to inject alternate amboy.Queue objects into
// constructed Runner objects. Returns an error if the Runner has
// started.
func (r *LocalWorkers) SetQueue(q amboy.Queue) error {
	if r.started {
		return errors.New("cannot add new queue after starting a runner")
	}

	r.queue = q
	return nil
}

// Started returns true when the Runner has begun executing tasks. For
// LocalWorkers this means that workers are running.
func (r *LocalWorkers) Started() bool {
	return r.started
}

func startWorkerServer(ctx context.Context, q amboy.Queue) <-chan amboy.Job {
	output := make(chan amboy.Job)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				job := q.Next(ctx)
				if job == nil {
					continue
				}

				if job.Status().Completed {
					grip.Debugf("job '%s' was dispatched from the queue but was completed",
						job.ID())
					continue
				}

				output <- job
			}
		}
	}()

	return output
}

func worker(ctx context.Context, jobs <-chan amboy.Job, q amboy.Queue) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-jobs:
			job.Run()
			q.Complete(ctx, job)
		}
	}
}

// Start initializes all worker process, and returns an error if the
// Runner has already started.
func (r *LocalWorkers) Start(ctx context.Context) error {
	if r.started {
		return nil
	}

	if r.queue == nil {
		return errors.New("runner must have an embedded queue")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	r.canceler = cancel
	jobs := startWorkerServer(workerCtx, r.queue)

	r.started = true
	grip.Debugf("running %d workers", r.size)

	for w := 1; w <= r.size; w++ {
		go func() {
			worker(workerCtx, jobs, r.queue)
		}()
		grip.Debugf("started worker %d of %d waiting for jobs", w, r.size)
	}

	return nil
}

// Close terminates all worker processes as soon as possible.
func (r *LocalWorkers) Close() {
	if r.canceler != nil {
		r.canceler()
	}
}
