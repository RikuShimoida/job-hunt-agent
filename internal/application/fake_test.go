package application_test

import (
	"context"
	"errors"
	"sync"

	"github.com/RikuShimoida/job-hunt-agent/internal/domain/model"
	"github.com/RikuShimoida/job-hunt-agent/internal/domain/port"
)

// errConnectorFailed はフェイクコネクタが返す失敗。
var errConnectorFailed = errors.New("fake connector failed")

// errSaveJobFailed はフェイクリポジトリが返す保存失敗。
var errSaveJobFailed = errors.New("fake repository failed to save job")

// errNotifyFailed はフェイク Notifier が返す送信失敗。
var errNotifyFailed = errors.New("fake notifier failed to send")

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

	jobs          []model.JobPosting
	runs          []model.CollectionRun
	notifications []model.Notification
	nextID        int64

	// saveJobErr は SaveJob が返す失敗。saveJobErrSource が空なら全ソースで、
	// 設定されていればその紹介元を持つ案件の保存だけが失敗する。
	saveJobErr       error
	saveJobErrSource string
	saveRunErr       error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1}
}

func (r *fakeRepository) SaveJob(_ context.Context, job *model.JobPosting) (port.SaveResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.saveJobErr != nil && r.failsSave(job) {
		return port.SaveResult{}, r.saveJobErr
	}

	for i := range r.jobs {
		if r.jobs[i].DedupKey != job.DedupKey {
			continue
		}

		changes := model.MaterialChanges(r.jobs[i], *job)

		// 採点結果は score の領分。実装（sqlite）と同じく収集では上書きしない。
		job.ID = r.jobs[i].ID
		job.Status = r.jobs[i].Status
		job.Score = r.jobs[i].Score
		job.ScoreReasons = r.jobs[i].ScoreReasons
		job.RejectionReasons = r.jobs[i].RejectionReasons
		job.FirstSeenAt = r.jobs[i].FirstSeenAt

		r.jobs[i] = *job
		return port.SaveResult{MaterialChanges: changes}, nil
	}

	job.ID = r.nextID
	r.nextID++
	r.jobs = append(r.jobs, *job)
	return port.SaveResult{Created: true}, nil
}

// failsSave は saveJobErr をこの案件へ適用するかを返す。
func (r *fakeRepository) failsSave(job *model.JobPosting) bool {
	if r.saveJobErrSource == "" {
		return true
	}
	for _, s := range job.Sources {
		if s.SourceName == r.saveJobErrSource {
			return true
		}
	}
	return false
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

func (r *fakeRepository) UpdateStatus(_ context.Context, jobID int64, status model.JobStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.jobs {
		if r.jobs[i].ID == jobID {
			r.jobs[i].Status = status
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

func (r *fakeRepository) SaveNotification(_ context.Context, n *model.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	n.ID = int64(len(r.notifications) + 1)
	r.notifications = append(r.notifications, *n)
	return nil
}

// ListNotifiedJobIDs は成功した通知だけを返す。昇順に上書きするため最新の hash が残る。
func (r *fakeRepository) ListNotifiedJobIDs(context.Context) (map[int64]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	notified := make(map[int64]string)
	for _, n := range r.notifications {
		if n.Result == model.NotificationResultSuccess {
			notified[n.JobID] = n.PayloadHash
		}
	}
	return notified, nil
}

func (r *fakeRepository) jobCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.jobs)
}

func (r *fakeRepository) jobByID(id int64) (model.JobPosting, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, j := range r.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return model.JobPosting{}, false
}

// updateRate は保存済み案件の単価を差し替える（重要変更の発生を再現する）。
func (r *fakeRepository) updateRate(id int64, min, max int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.jobs {
		if r.jobs[i].ID == id {
			r.jobs[i].RateMin = &min
			r.jobs[i].RateMax = &max
			return
		}
	}
}

// updateScoreReasons は加点理由の文言だけを差し替える（重要変更ではない変化）。
func (r *fakeRepository) updateScoreReasons(id int64, reasons []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.jobs {
		if r.jobs[i].ID == id {
			r.jobs[i].ScoreReasons = reasons
			return
		}
	}
}

func (r *fakeRepository) notificationsFor(jobID int64) []model.Notification {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []model.Notification
	for _, n := range r.notifications {
		if n.JobID == jobID {
			out = append(out, n)
		}
	}
	return out
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

// fakeNotifier は渡された案件を記録する Notifier。
// failTitles に含まれる案件名は送信失敗として記録し、部分失敗を再現する。
type fakeNotifier struct {
	mu sync.Mutex

	notified   []port.NotifyItem
	calls      int
	failTitles map[string]struct{}
}

func (n *fakeNotifier) Name() string { return "fake" }

func (n *fakeNotifier) Notify(_ context.Context, items []port.NotifyItem) ([]model.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if len(items) == 0 {
		return nil, nil
	}

	n.calls++
	n.notified = append(n.notified, items...)

	records := make([]model.Notification, 0, len(items))
	for _, item := range items {
		rec := model.Notification{
			JobID:       item.Job.ID,
			Channel:     n.Name(),
			PayloadHash: model.MaterialHash(item.Job),
			Result:      model.NotificationResultSuccess,
		}
		if _, fail := n.failTitles[item.Job.Title]; fail {
			rec.Result = model.NotificationResultFailed
			rec.ErrorMessage = errNotifyFailed.Error()
		}
		records = append(records, rec)
	}
	return records, nil
}

func (n *fakeNotifier) notifiedItems() []port.NotifyItem {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := make([]port.NotifyItem, len(n.notified))
	copy(out, n.notified)
	return out
}

// reset は次の Notify だけを観測できるよう記録を捨てる。
func (n *fakeNotifier) reset() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.notified = nil
	n.calls = 0
}

func (n *fakeNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

// fakeErrorNotifier は通知されたソース失敗を記録する。
type fakeErrorNotifier struct {
	mu sync.Mutex

	failures []model.SourceFailure
	calls    int
}

func (n *fakeErrorNotifier) NotifyError(_ context.Context, failures []model.SourceFailure) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.calls++
	n.failures = append(n.failures, failures...)
	return nil
}

func (n *fakeErrorNotifier) notified() []model.SourceFailure {
	n.mu.Lock()
	defer n.mu.Unlock()

	out := make([]model.SourceFailure, len(n.failures))
	copy(out, n.failures)
	return out
}

func (n *fakeErrorNotifier) callCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}
