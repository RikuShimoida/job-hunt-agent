package application_test

import (
	"context"
	"errors"
	"sync"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
)

// errConnectorFailed はフェイクコネクタが返す失敗。
var errConnectorFailed = errors.New("fake connector failed")

// fakeConnector は固定の RawJob を返すコネクタ。err を設定すると必ず失敗する。
type fakeConnector struct {
	name      string
	raws      []model.RawJob
	err       error
	callCount int
}

func (c *fakeConnector) Name() string { return c.name }

func (c *fakeConnector) Fetch(context.Context) ([]model.RawJob, error) {
	c.callCount++
	if c.err != nil {
		return nil, c.err
	}
	return c.raws, nil
}

// fakeRepository はインメモリのリポジトリ。dedup_key で重複を弾く。
type fakeRepository struct {
	mu sync.Mutex

	jobs   []model.JobPosting
	runs   []model.CollectionRun
	nextID int64

	saveJobErr error
	saveRunErr error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1}
}

func (r *fakeRepository) SaveJob(_ context.Context, job *model.JobPosting) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.saveJobErr != nil {
		return false, r.saveJobErr
	}
	for i := range r.jobs {
		if r.jobs[i].DedupKey == job.DedupKey {
			r.jobs[i].LastSeenAt = job.LastSeenAt
			job.ID = r.jobs[i].ID
			return false, nil
		}
	}
	job.ID = r.nextID
	r.nextID++
	r.jobs = append(r.jobs, *job)
	return true, nil
}

func (r *fakeRepository) ListJobs(context.Context) ([]model.JobPosting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]model.JobPosting, len(r.jobs))
	copy(out, r.jobs)
	return out, nil
}

func (r *fakeRepository) UpdateScore(_ context.Context, job *model.JobPosting) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.jobs {
		if r.jobs[i].ID == job.ID {
			r.jobs[i].Score = job.Score
			r.jobs[i].ScoreReasons = job.ScoreReasons
			r.jobs[i].RejectionReasons = job.RejectionReasons
			r.jobs[i].Status = job.Status
			return nil
		}
	}
	return nil
}

func (r *fakeRepository) SaveRun(_ context.Context, run *model.CollectionRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.saveRunErr != nil {
		return r.saveRunErr
	}
	r.runs = append(r.runs, *run)
	return nil
}

func (r *fakeRepository) jobCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

func (r *fakeRepository) runsFor(source string) []model.CollectionRun {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []model.CollectionRun
	for _, run := range r.runs {
		if run.SourceName == source {
			out = append(out, run)
		}
	}
	return out
}

// fakeNotifier は通知された案件を記録するだけの Notifier。
type fakeNotifier struct {
	mu       sync.Mutex
	notified []model.JobPosting
	calls    int
}

func (n *fakeNotifier) Notify(_ context.Context, jobs []model.JobPosting) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.calls++
	n.notified = append(n.notified, jobs...)
	return nil
}

func (n *fakeNotifier) notifiedJobs() []model.JobPosting {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := make([]model.JobPosting, len(n.notified))
	copy(out, n.notified)
	return out
}
