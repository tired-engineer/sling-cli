package sling

import (
	"bufio"
	"os"
	"strings"

	"github.com/flarco/dbio"
	"github.com/flarco/dbio/database"
	"github.com/flarco/dbio/filesys"
	"github.com/flarco/dbio/iop"
	"github.com/flarco/dbio/saas/airbyte"
	"github.com/flarco/g"
	"github.com/samber/lo"
	"github.com/spf13/cast"
)

// ReadFromDB reads from a source database
func (t *TaskExecution) ReadFromDB(cfg *Config, srcConn database.Connection) (df *iop.Dataflow, err error) {

	fieldsStr := "*"
	sTable, err := database.ParseTableName(cfg.Source.Stream, srcConn.GetType())
	if err != nil {
		err = g.Error(err, "Could not parse source stream text")
		return t.df, err
	} else if sTable.Schema == "" {
		sTable.Schema = cast.ToString(cfg.Source.Data["schema"])
	}

	// check if referring to a SQL file
	if schemeType(cfg.Source.Stream).IsFile() && g.PathExists(strings.TrimPrefix(cfg.Source.Stream, "file://")) {
		// for incremental, need to put `{incremental_where_cond}` for proper selecting
		sqlFromFile, err := getSQLText(cfg.Source.Stream)
		if err != nil {
			err = g.Error(err, "Could not get getSQLText for: "+cfg.Source.Stream)
			if sTable.Name == "" {
				return t.df, err
			} else {
				err = nil // don't return error in case the table full name ends with .sql
				g.LogError(err)
			}
		} else {
			cfg.Source.Stream = sqlFromFile
			sTable.SQL = sqlFromFile
		}
	}

	if len(cfg.Source.Columns) > 0 {
		fields := lo.Map(cfg.Source.Columns, func(f string, i int) string {
			return f
		})
		fieldsStr = strings.Join(fields, ", ")
	}

	// Get source columns
	// if sTable.IsQuery() {
	// 	cfg.Source.columns, err = srcConn.GetSQLColumns(g.R(sTable.SQL, "incremental_where_cond", "1=0"))
	// } else {
	// 	cfg.Source.columns, err = srcConn.GetTableColumns(sTable)
	// }
	// if err != nil {
	// 	err = g.Error(err, "Could not obtain source columns")
	// 	return t.df, err
	// }

	// if cfg.Mode == IncrementalMode || (cfg.Mode == AppendMode && cfg.Source.UpdateKey != "") {
	if t.usingCheckpoint() {
		// select only records that have been modified after last max value
		incrementalWhereCond := "1=1"
		if cfg.IncrementalVal != "" {
			greaterThan := ">="
			if val := os.Getenv("SLING_GREATER_THAN_EQUAL"); val != "" {
				greaterThan = lo.Ternary(cast.ToBool(val), ">=", ">")
			}
			incrementalWhereCond = g.R(
				"{update_key} {gt} {value}",
				"update_key", cfg.Source.UpdateKey,
				"value", cfg.IncrementalVal,
				"gt", greaterThan,
			)
		}

		if sTable.SQL == "" {
			sTable.SQL = g.R(
				`select {fields} from {table} where {incremental_where_cond}`,
				"fields", fieldsStr,
				"table", sTable.FDQN(),
				"incremental_where_cond", incrementalWhereCond,
			)
		} else {
			sTable.SQL = g.R(
				sTable.SQL,
				"incremental_where_cond", incrementalWhereCond,
				"update_key", cfg.Source.UpdateKey,
				"incremental_value", cfg.IncrementalVal,
			)
		}
	} else if cfg.Source.Limit > 0 && sTable.SQL == "" {
		sTable.SQL = g.R(
			srcConn.Template().Core["limit"],
			"fields", fieldsStr,
			"table", sTable.FDQN(),
			"limit", cast.ToString(cfg.Source.Limit),
		)
	}

	if srcConn.GetType() == dbio.TypeDbBigTable {
		srcConn.SetProp("start_time", t.Config.IncrementalVal)
	}

	df, err = srcConn.BulkExportFlow(sTable)
	if err != nil {
		err = g.Error(err, "Could not BulkExportFlow: "+sTable.Select())
		return t.df, err
	}

	return
}

// ReadFromAPI reads from a source API
func (t *TaskExecution) ReadFromAPI(cfg *Config, client *airbyte.Airbyte) (df *iop.Dataflow, err error) {

	df = iop.NewDataflow()
	var stream *iop.Datastream

	if cfg.SrcConn.Type.IsAirbyte() {
		config := airbyte.StreamConfig{
			Columns:   cfg.Source.Columns,
			StartDate: cfg.IncrementalVal,
		}
		stream, err = client.Stream(cfg.Source.Stream, config)
		if err != nil {
			err = g.Error(err, "Could not read stream '%s' for connection: %s", cfg.Source.Stream, cfg.SrcConn.Type)
			return t.df, err
		}

		df, err = iop.MakeDataFlow(stream)
		if err != nil {
			err = g.Error(err, "Could not MakeDataFlow")
			return t.df, err
		}
	} else {
		err = g.Error("API type not implemented: %s", cfg.SrcConn.Type)
	}

	return
}

// ReadFromFile reads from a source file
func (t *TaskExecution) ReadFromFile(cfg *Config) (df *iop.Dataflow, err error) {

	var stream *iop.Datastream
	options := t.sourceOptionsMap()

	if cfg.SrcConn.URL() != "" {
		// construct props by merging with options
		options["SLING_FS_TIMESTAMP"] = t.Config.IncrementalVal
		props := append(
			g.MapToKVArr(cfg.SrcConn.DataS()),
			g.MapToKVArr(g.ToMapString(options))...,
		)

		fs, err := filesys.NewFileSysClientFromURLContext(t.Context.Ctx, cfg.SrcConn.URL(), props...)
		if err != nil {
			err = g.Error(err, "Could not obtain client for %s ", cfg.SrcConn.Type)
			return t.df, err
		}

		fsCfg := filesys.FileStreamConfig{Columns: cfg.Source.Columns, Limit: cfg.Source.Limit}
		df, err = fs.ReadDataflow(cfg.SrcConn.URL(), fsCfg)
		if err != nil {
			err = g.Error(err, "Could not FileSysReadDataflow for %s", cfg.SrcConn.Type)
			return t.df, err
		}
	} else {
		stream, err = filesys.MakeDatastream(bufio.NewReader(os.Stdin), g.ToMapString(options))
		if err != nil {
			err = g.Error(err, "Could not MakeDatastream")
			return t.df, err
		}
		df, err = iop.MakeDataFlow(stream.Split()...)
		if err != nil {
			err = g.Error(err, "Could not MakeDataFlow for Stdin")
			return t.df, err
		}
	}

	if len(df.Streams) == 0 {
		streamName := lo.Ternary(cfg.SrcConn.URL() == "", "stdin", cfg.SrcConn.URL())
		return df, g.Error("Could not read stream (%s)", streamName)
	} else if len(df.Columns) == 0 && !df.Streams[0].IsClosed() {
		return df, g.Error("Could not read columns")
	}

	return
}
