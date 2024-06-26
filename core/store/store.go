package store

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flarco/g"
	"github.com/flarco/g/net"
	"github.com/slingdata-io/sling-cli/core"
	"github.com/slingdata-io/sling-cli/core/dbio/connection"
	"github.com/slingdata-io/sling-cli/core/sling"
	"github.com/spf13/cast"
	"gorm.io/gorm/clause"
)

func init() {
	sling.StoreInsert = StoreInsert
	sling.StoreUpdate = StoreUpdate
}

// Execution is a task execute in the store. PK = exec_id + stream_id
type Execution struct {
	// ID auto-increments
	ID int64 `json:"id,omitempty" gorm:"primaryKey"`

	ExecID string `json:"exec_id,omitempty" gorm:"index"`

	// StreamID represents the stream inside the replication that is running.
	// Is an MD5 construct:`md5(Source, Target, Stream)`.
	StreamID string `json:"stream_id,omitempty" sql:"not null" gorm:"index"`

	// ConfigMD5 points to config table. not null
	TaskMD5        string `json:"task_md5,omitempty" sql:"not null" gorm:"index"`
	ReplicationMD5 string `json:"replication_md5,omitempty" sql:"not null" gorm:"index"`

	Status    sling.ExecStatus `json:"status,omitempty" gorm:"index"`
	Err       *string          `json:"error,omitempty"`
	StartTime *time.Time       `json:"start_time,omitempty" gorm:"index"`
	EndTime   *time.Time       `json:"end_time,omitempty" gorm:"index"`
	Bytes     uint64           `json:"bytes,omitempty"`
	ExitCode  int              `json:"exit_code,omitempty"`
	Output    string           `json:"output,omitempty" sql:"default ''"`
	Rows      uint64           `json:"rows,omitempty"`
	Pid       int              `json:"pid,omitempty"`
	Version   string           `json:"version,omitempty"`

	// ProjectID represents the project or the repository.
	// If .git exists, grab first commit with `git rev-list --max-parents=0 HEAD`.
	// if not, use md5 of path of folder. Can be `null` if using task.
	ProjectID *string `json:"project_id,omitempty" gorm:"index"`

	// FilePath represents the path to a file.
	// We would need this to refer back to the same file, even if
	// the contents change. So this should just be the relative path
	// of the replication.yaml or task.yaml from the root of the project.
	// If Ad-hoc from CLI flags, let it be `null`.
	FilePath *string `json:"file_path,omitempty" gorm:"index"`

	CreatedDt time.Time `json:"created_dt,omitempty" gorm:"autoCreateTime"`
	UpdatedDt time.Time `json:"updated_dt,omitempty" gorm:"autoUpdateTime"`

	Task        *Task        `json:"task,omitempty" gorm:"-"`
	Replication *Replication `json:"replication,omitempty" gorm:"-"`
}

type Task struct {
	ProjectID *string `json:"project_id,omitempty" gorm:"index"`

	// MD5 is MD5 of Config json string
	MD5 string `json:"md5" gorm:"primaryKey"`

	Type sling.JobType `json:"type"  gorm:"index"`

	Task sling.Config `json:"task"`

	CreatedDt time.Time `json:"created_dt" gorm:"autoCreateTime"`
	UpdatedDt time.Time `json:"updated_dt" gorm:"autoUpdateTime"`
}

type Replication struct {
	Name string `json:"name"  gorm:"index"`

	ProjectID *string `json:"project_id,omitempty" gorm:"index"`

	// MD5 is MD5 of Config json string
	MD5 string `json:"md5" gorm:"primaryKey"`

	Type sling.JobType `json:"type"  gorm:"index"`

	Replication sling.ReplicationConfig `json:"replication"`

	CreatedDt time.Time `json:"created_dt" gorm:"autoCreateTime"`
	UpdatedDt time.Time `json:"updated_dt" gorm:"autoUpdateTime"`
}

// Store saves the task into the local sqlite
func ToExecutionObject(t *sling.TaskExecution) *Execution {

	bytes, _ := t.GetBytes()

	exec := Execution{
		ExecID:         t.ExecID,
		StreamID:       g.MD5(t.Config.Source.Conn, t.Config.Target.Conn, t.Config.Source.Stream),
		Status:         t.Status,
		StartTime:      t.StartTime,
		EndTime:        t.EndTime,
		Bytes:          bytes,
		Output:         t.Output,
		Rows:           t.GetCount(),
		ProjectID:      g.String(t.Config.Env["SLING_PROJECT_ID"]),
		FilePath:       g.String(t.Config.Env["SLING_CONFIG_PATH"]),
		ReplicationMD5: os.Getenv("SLING_REPLICATION_MD5"),
		Pid:            os.Getpid(),
		Version:        core.Version,
	}

	if t.Err != nil {
		err, ok := t.Err.(*g.ErrType)
		if ok {
			exec.Err = g.String(err.Full())
		} else {
			exec.Err = g.String(t.Err.Error())
		}
	}

	if t.Replication != nil && t.Replication.Env["SLING_CONFIG_PATH"] != nil {
		exec.FilePath = g.String(cast.ToString(t.Replication.Env["SLING_CONFIG_PATH"]))
	} else if fileID := os.Getenv("SLING_REPLICATION_ID"); fileID != "" {
		exec.FilePath = g.String(fileID)
	}

	return &exec
}

func ToConfigObject(t *sling.TaskExecution) (task *Task, replication *Replication) {
	if t.Config == nil {
		return
	}

	task = &Task{
		Type: t.Type,
		Task: *t.Config,
	}

	projID := t.Config.Env["SLING_PROJECT_ID"]
	if projID != "" {
		task.ProjectID = g.String(projID)
	}

	if t.Replication != nil {
		replication = &Replication{
			Name:        t.Config.Env["SLING_CONFIG_PATH"],
			Type:        t.Type,
			MD5:         t.Replication.MD5(),
			Replication: *t.Replication,
		}

		if projID != "" {
			replication.ProjectID = g.String(projID)
		}
	}

	// clean up
	if strings.Contains(task.Task.Source.Conn, "://") {
		task.Task.Source.Conn = strings.Split(task.Task.Source.Conn, "://")[0] + "://"
	}

	if strings.Contains(task.Task.Target.Conn, "://") {
		task.Task.Target.Conn = strings.Split(task.Task.Target.Conn, "://")[0] + "://"
	}

	task.Task.Source.Data = nil
	task.Task.Target.Data = nil

	task.Task.SrcConn = connection.Connection{}
	task.Task.TgtConn = connection.Connection{}

	task.Task.Prepared = false

	delete(task.Task.Env, "SLING_PROJECT_ID")
	delete(task.Task.Env, "SLING_CONFIG_PATH")

	// set md5
	task.MD5 = t.Config.MD5()

	return
}

// Store saves the task into the local sqlite
func StoreInsert(t *sling.TaskExecution) {
	if Db == nil {
		return
	}

	// make execution
	exec := ToExecutionObject(t)

	// insert config
	task, replication := ToConfigObject(t)
	err := Db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(task).Error
	if err != nil {
		g.DebugLow("could not insert task config into local .sling.db. %s", err.Error())
		return
	}
	exec.Task = task
	exec.TaskMD5 = task.MD5

	if replication != nil {
		err := Db.Clauses(clause.OnConflict{DoNothing: true}).
			Create(replication).Error
		if err != nil {
			g.DebugLow("could not insert replication config into local .sling.db. %s", err.Error())
			return
		}
		exec.Replication = replication
		exec.ReplicationMD5 = replication.MD5
	}

	// insert execution
	err = Db.Create(exec).Error
	if err != nil {
		g.DebugLow("could not insert execution into local .sling.db. %s", err.Error())
		return
	}

	t.ExecID = exec.ExecID

	// send status
	sendStatus(*exec)
}

// Store saves the task into the local sqlite
func StoreUpdate(t *sling.TaskExecution) {
	if Db == nil {
		return
	}
	e := ToExecutionObject(t)

	exec := &Execution{ExecID: t.ExecID, StreamID: e.StreamID}
	err := Db.Where("exec_id = ? and stream_id = ?", t.ExecID, e.StreamID).First(exec).Error
	if err != nil {
		g.DebugLow("could not select execution from local .sling.db. %s", err.Error())
		return
	}

	exec.StartTime = e.StartTime
	exec.EndTime = e.EndTime
	exec.Status = e.Status
	exec.Err = e.Err
	exec.Bytes = e.Bytes
	exec.Rows = e.Rows
	exec.Output = e.Output

	err = Db.Updates(exec).Error
	if err != nil {
		g.DebugLow("could not update execution into local .sling.db. %s", err.Error())
		return
	}

	// send status
	sendStatus(*exec)
}

func sendStatus(exec Execution) {
	if os.Getenv("SLING_STATUS_URL") == "" {
		return
	}

	headers := map[string]string{"Content-Type": "application/json"}
	exec.Output = "" // no need for output
	payload := g.Marshal(exec)
	net.ClientDo(
		http.MethodPost, os.Getenv("SLING_STATUS_URL"),
		strings.NewReader(payload), headers, 3,
	)
}
