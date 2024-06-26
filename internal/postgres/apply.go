package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dalibo/ldap2pg/internal"
	"github.com/dalibo/ldap2pg/internal/perf"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/exp/slices"
)

var formatter = FmtQueryRewriter{}

func Apply(ctx context.Context, watch *perf.StopWatch, diff <-chan SyncQuery, real bool) (count int, err error) {
	prefix := ""
	if !real {
		prefix = "Would "
	}

	for query := range diff {
		if !slices.ContainsFunc(query.LogArgs, func(i interface{}) bool {
			return i == "database"
		}) {
			query.LogArgs = append(query.LogArgs, "database", query.Database)
		}
		slog.Log(ctx, internal.LevelChange, prefix+query.Description, query.LogArgs...)
		count++
		pgConn, err := GetConn(ctx, query.Database)
		if err != nil {
			return count, fmt.Errorf("PostgreSQL error: %w", err)
		}

		// Rewrite query to log a pasteable query even when in Dry mode.
		sql, _, _ := formatter.RewriteQuery(ctx, pgConn, query.Query, query.QueryArgs)
		slog.Debug(prefix + "Execute SQL query:\n" + sql)

		if !real {
			continue
		}

		var tag pgconn.CommandTag
		duration := watch.TimeIt(func() {
			_, err = pgConn.Exec(ctx, sql)
		})
		if err != nil {
			return count, fmt.Errorf("sync: %w", err)
		}
		slog.Debug("Query terminated.", "duration", duration, "rows", tag.RowsAffected())
	}
	return
}
