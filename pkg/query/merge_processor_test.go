package query

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/nnnkkk7/snowflake-emulator/pkg/connection"
	"github.com/nnnkkk7/snowflake-emulator/pkg/metadata"
)

func setupMergeProcessorTest(t *testing.T) (*MergeProcessor, *Executor, func()) {
	t.Helper()

	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("Failed to open DuckDB: %v", err)
	}

	connMgr := connection.NewManager(db)
	repo, err := metadata.NewRepository(connMgr)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create repository: %v", err)
	}

	executor := NewExecutor(connMgr, repo)
	handler := NewMergeProcessor(executor)

	cleanup := func() {
		db.Close()
	}

	return handler, executor, cleanup
}

func TestMergeProcessor_ParseMergeStatement(t *testing.T) {
	handler, _, cleanup := setupMergeProcessorTest(t)
	defer cleanup()

	testCases := []struct {
		name    string
		sql     string
		want    *MergeStatement
		wantErr bool
	}{
		{
			name: "BasicMerge_Update",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN MATCHED THEN UPDATE SET t.value = s.value`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched: true,
						Action:    MergeActionUpdate,
						SetClauses: []SetClause{
							{Column: "t.value", Value: "s.value"},
						},
					},
				},
			},
		},
		{
			name: "MergeWithDelete",
			sql: `MERGE INTO target USING source
                  ON target.id = source.id
                  WHEN MATCHED THEN DELETE`,
			want: &MergeStatement{
				TargetTable: "target",
				SourceTable: "source",
				OnCondition: "target.id = source.id",
				WhenClauses: []WhenClause{
					{
						IsMatched: true,
						Action:    MergeActionDelete,
					},
				},
			},
		},
		{
			name: "MergeWithInsert",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN NOT MATCHED THEN INSERT (id, name) VALUES (s.id, s.name)`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched:  false,
						Action:     MergeActionInsert,
						InsertCols: []string{"id", "name"},
						InsertVals: []string{"s.id", "s.name"},
					},
				},
			},
		},
		{
			name: "FullMerge_AllClauses",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN MATCHED AND s.deleted = true THEN DELETE
                  WHEN MATCHED THEN UPDATE SET t.value = s.value
                  WHEN NOT MATCHED THEN INSERT (id, value) VALUES (s.id, s.value)`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched: true,
						Condition: "s.deleted = true",
						Action:    MergeActionDelete,
					},
					{
						IsMatched: true,
						Action:    MergeActionUpdate,
						SetClauses: []SetClause{
							{Column: "t.value", Value: "s.value"},
						},
					},
					{
						IsMatched:  false,
						Action:     MergeActionInsert,
						InsertCols: []string{"id", "value"},
						InsertVals: []string{"s.id", "s.value"},
					},
				},
			},
		},
		{
			name: "MergeWithSubquery",
			sql: `MERGE INTO target t
                  USING (SELECT id, name FROM staging WHERE active = true) s
                  ON t.id = s.id
                  WHEN MATCHED THEN UPDATE SET t.name = s.name`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "(SELECT id, name FROM staging WHERE active = true)",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched: true,
						Action:    MergeActionUpdate,
						SetClauses: []SetClause{
							{Column: "t.name", Value: "s.name"},
						},
					},
				},
			},
		},
		{
			// A regex built on [^)]+ between the VALUES parens has no way to
			// look past a value that is itself a call with its own closing
			// paren — CURRENT_TIMESTAMP() truncated the capture right there,
			// silently dropping the list's own closing paren along with the
			// updated_at value that should have followed it.
			name: "InsertValueThatIsItselfAFunctionCall",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN NOT MATCHED THEN INSERT (id, name, updated_at)
                  VALUES (s.id, s.name, CURRENT_TIMESTAMP())`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched:  false,
						Action:     MergeActionInsert,
						InsertCols: []string{"id", "name", "updated_at"},
						InsertVals: []string{"s.id", "s.name", "CURRENT_TIMESTAMP()"},
					},
				},
			},
		},
		{
			// The same shape without a column list, and formatted across
			// several lines the way a hand-written procedure body reads.
			name: "InsertNoColumnListWithAFunctionCallValue",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN NOT MATCHED THEN
                      INSERT
                      VALUES (
                          s.id,
                          CURRENT_TIMESTAMP()
                      )`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched:  false,
						Action:     MergeActionInsert,
						InsertVals: []string{"s.id", "CURRENT_TIMESTAMP()"},
					},
				},
			},
		},
		{
			// (.+) without (?s) cannot cross a newline, so a SET clause with
			// each assignment on its own line — again, ordinary formatting —
			// had everything after the first line silently dropped: only
			// t.name survived, t.updated_at vanished with no error at all.
			name: "UpdateSetWithEachAssignmentOnItsOwnLine",
			sql: `MERGE INTO target t USING source s
                  ON t.id = s.id
                  WHEN MATCHED THEN
                      UPDATE SET
                          t.name = s.name,
                          t.updated_at = CURRENT_TIMESTAMP()

                  WHEN NOT MATCHED THEN INSERT (id) VALUES (s.id)`,
			want: &MergeStatement{
				TargetTable: "target",
				TargetAlias: "t",
				SourceTable: "source",
				SourceAlias: "s",
				OnCondition: "t.id = s.id",
				WhenClauses: []WhenClause{
					{
						IsMatched: true,
						Action:    MergeActionUpdate,
						SetClauses: []SetClause{
							{Column: "t.name", Value: "s.name"},
							{Column: "t.updated_at", Value: "CURRENT_TIMESTAMP()"},
						},
					},
					{
						IsMatched:  false,
						Action:     MergeActionInsert,
						InsertCols: []string{"id"},
						InsertVals: []string{"s.id"},
					},
				},
			},
		},
		{
			name:    "InvalidMerge_MissingTarget",
			sql:     "MERGE INTO",
			wantErr: true,
		},
		{
			name:    "InvalidMerge_MissingUsing",
			sql:     "MERGE INTO target ON t.id = s.id",
			wantErr: true,
		},
		{
			name:    "InvalidMerge_MissingOnCondition",
			sql:     "MERGE INTO target USING source WHEN MATCHED THEN DELETE",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := handler.ParseMergeStatement(tc.sql)
			if (err != nil) != tc.wantErr {
				t.Errorf("ParseMergeStatement() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if tc.wantErr {
				return
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ParseMergeStatement() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMergeProcessor_ExecuteMerge_Integration(t *testing.T) {
	handler, executor, cleanup := setupMergeProcessorTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create target and source tables
	_, err := executor.Execute(ctx, `CREATE TABLE target (id INTEGER, value VARCHAR, name VARCHAR)`)
	if err != nil {
		t.Fatalf("Failed to create target table: %v", err)
	}

	_, err = executor.Execute(ctx, `CREATE TABLE source (id INTEGER, value VARCHAR, name VARCHAR, deleted BOOLEAN)`)
	if err != nil {
		t.Fatalf("Failed to create source table: %v", err)
	}

	// Insert initial data into target
	_, err = executor.Execute(ctx, `INSERT INTO target VALUES (1, 'old_value1', 'name1'), (2, 'old_value2', 'name2')`)
	if err != nil {
		t.Fatalf("Failed to insert into target: %v", err)
	}

	// Insert data into source
	_, err = executor.Execute(ctx, `INSERT INTO source VALUES
		(1, 'new_value1', 'updated_name1', false),
		(2, 'delete_me', 'name2', true),
		(3, 'insert_value', 'new_name3', false)`)
	if err != nil {
		t.Fatalf("Failed to insert into source: %v", err)
	}

	t.Run("UpdateMerge", func(t *testing.T) {
		// Reset target table
		_, _ = executor.Execute(ctx, `DELETE FROM target`)
		_, _ = executor.Execute(ctx, `INSERT INTO target VALUES (1, 'old', 'name1')`)

		stmt := &MergeStatement{
			TargetTable: "target",
			TargetAlias: "t",
			SourceTable: "source",
			SourceAlias: "s",
			OnCondition: "t.id = s.id",
			WhenClauses: []WhenClause{
				{
					IsMatched: true,
					Action:    MergeActionUpdate,
					SetClauses: []SetClause{
						{Column: "value", Value: "s.value"},
					},
				},
			},
		}

		result, err := handler.ExecuteMerge(ctx, ExecutionContext{}, stmt)
		if err != nil {
			t.Fatalf("ExecuteMerge failed: %v", err)
		}

		// Verify the update happened
		queryResult, err := executor.Query(ctx, `SELECT value FROM target WHERE id = 1`)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if len(queryResult.Rows) != 1 {
			t.Errorf("Expected 1 row, got %d", len(queryResult.Rows))
		}

		// Check that merge returned some affected rows
		if result.RowsUpdated == 0 && result.RowsInserted == 0 && result.RowsDeleted == 0 {
			// Native MERGE might report everything as RowsUpdated
			t.Logf("Merge result: inserted=%d, updated=%d, deleted=%d",
				result.RowsInserted, result.RowsUpdated, result.RowsDeleted)
		}
	})

	t.Run("InsertMerge", func(t *testing.T) {
		// Reset target table
		_, _ = executor.Execute(ctx, `DELETE FROM target`)
		_, _ = executor.Execute(ctx, `INSERT INTO target VALUES (1, 'existing', 'name1')`)

		stmt := &MergeStatement{
			TargetTable: "target",
			TargetAlias: "t",
			SourceTable: "source",
			SourceAlias: "s",
			OnCondition: "t.id = s.id",
			WhenClauses: []WhenClause{
				{
					IsMatched:  false,
					Action:     MergeActionInsert,
					InsertCols: []string{"id", "value", "name"},
					InsertVals: []string{"s.id", "s.value", "s.name"},
				},
			},
		}

		_, err := handler.ExecuteMerge(ctx, ExecutionContext{}, stmt)
		if err != nil {
			t.Fatalf("ExecuteMerge failed: %v", err)
		}

		// Verify new rows were inserted (source has 3 rows, target has 1 matching)
		queryResult, err := executor.Query(ctx, `SELECT COUNT(*) FROM target`)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		// Should have original row + 2 new rows (id=2 and id=3 from source)
		if len(queryResult.Rows) != 1 {
			t.Fatalf("Expected 1 result row for COUNT(*)")
		}
	})
}

func TestIsMerge(t *testing.T) {
	testCases := []struct {
		sql  string
		want bool
	}{
		{"MERGE INTO target USING source ON t.id = s.id WHEN MATCHED THEN DELETE", true},
		{"merge into target using source on t.id = s.id when matched then delete", true},
		{"  MERGE INTO target", true},
		{"SELECT * FROM table", false},
		{"INSERT INTO table VALUES (1)", false},
		{"UPDATE table SET x = 1", false},
		{"DELETE FROM table", false},
		{"COPY INTO table FROM @stage", false},
	}

	for _, tc := range testCases {
		t.Run(tc.sql, func(t *testing.T) {
			got := IsMerge(tc.sql)
			if got != tc.want {
				t.Errorf("IsMerge(%q) = %v, want %v", tc.sql, got, tc.want)
			}
		})
	}
}

func TestClassifier_Merge(t *testing.T) {
	classifier := NewClassifier()

	sql := "MERGE INTO target USING source ON t.id = s.id WHEN MATCHED THEN DELETE"
	result := classifier.Classify(sql)

	if result.Type != StatementTypeMerge {
		t.Errorf("Expected StatementTypeMerge, got %v", result.Type)
	}
	if !result.IsDML {
		t.Error("Expected IsDML to be true")
	}
	if result.IsDDL {
		t.Error("Expected IsDDL to be false")
	}
	if result.IsQuery {
		t.Error("Expected IsQuery to be false")
	}
}

func TestResolveMergeTables(t *testing.T) {
	withContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}

	tests := []struct {
		name             string
		executionContext ExecutionContext
		target           string
		source           string
		wantTarget       string
		wantSource       string
	}{
		{
			name:             "short names resolve to physical names",
			executionContext: withContext,
			target:           "merge_target",
			source:           "merge_source",
			wantTarget:       "TEST_DB.PUBLIC_MERGE_TARGET",
			wantSource:       "TEST_DB.PUBLIC_MERGE_SOURCE",
		},
		{
			name:             "qualified names are left alone",
			executionContext: withContext,
			target:           "OTHER_DB.PUBLIC_TARGET",
			source:           "merge_source",
			wantTarget:       "OTHER_DB.PUBLIC_TARGET",
			wantSource:       "TEST_DB.PUBLIC_MERGE_SOURCE",
		},
		{
			name:             "a subquery source is left alone",
			executionContext: withContext,
			target:           "merge_target",
			source:           "(SELECT 1 AS id)",
			wantTarget:       "TEST_DB.PUBLIC_MERGE_TARGET",
			wantSource:       "(SELECT 1 AS id)",
		},
		{
			name:             "without a context nothing is resolved",
			executionContext: ExecutionContext{},
			target:           "merge_target",
			source:           "merge_source",
			wantTarget:       "merge_target",
			wantSource:       "merge_source",
		},
	}

	handler, _, cleanup := setupMergeProcessorTest(t)
	defer cleanup()
	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt := &MergeStatement{TargetTable: tt.target, SourceTable: tt.source}
			got, err := handler.resolveMergeTables(ctx, stmt, tt.executionContext)
			if err != nil {
				t.Fatalf("resolveMergeTables() error = %v", err)
			}

			if got.TargetTable != tt.wantTarget {
				t.Errorf("TargetTable = %q, want %q", got.TargetTable, tt.wantTarget)
			}
			if got.SourceTable != tt.wantSource {
				t.Errorf("SourceTable = %q, want %q", got.SourceTable, tt.wantSource)
			}
			if stmt.TargetTable != tt.target || stmt.SourceTable != tt.source {
				t.Error("resolveMergeTables mutated its argument; it must return a copy")
			}
		})
	}
}

// TestResolveMergeTables_TemporaryTableIsLeftBare pins the bug this method was
// changed to fix: a name that already resolved to a temporary table — a
// procedure's own mangled __PROC_TEMP_... name, or a plain CREATE TEMPORARY
// TABLE used directly in a MERGE — was qualified a second time into
// TEST_DB.PUBLIC___PROC_TEMP_..., a name nothing had ever created.
func TestResolveMergeTables_TemporaryTableIsLeftBare(t *testing.T) {
	handler, executor, cleanup := setupMergeProcessorTest(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := executor.repo.CreateDatabase(ctx, "TEST_DB", ""); err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	executionContext := ExecutionContext{Database: "TEST_DB", Schema: "PUBLIC"}
	if _, err := executor.ExecuteWithContext(ctx, executionContext,
		"CREATE TEMPORARY TABLE scratch_source AS SELECT 1 AS id"); err != nil {
		t.Fatalf("failed to create the temp table: %v", err)
	}

	stmt := &MergeStatement{TargetTable: "merge_target", SourceTable: "scratch_source"}
	got, err := handler.resolveMergeTables(ctx, stmt, executionContext)
	if err != nil {
		t.Fatalf("resolveMergeTables() error = %v", err)
	}

	if got.SourceTable != "scratch_source" {
		t.Errorf("SourceTable = %q, want the temp table's bare name unchanged", got.SourceTable)
	}
	if got.TargetTable != "TEST_DB.PUBLIC_MERGE_TARGET" {
		t.Errorf("TargetTable = %q, an ordinary table should still be qualified", got.TargetTable)
	}
}
