// Package query provides SQL query execution and classification.
package query

import (
	"strings"

	"github.com/nnnkkk7/snowflake-emulator/pkg/config"
)

// StatementType represents the category of a SQL statement.
type StatementType int

// Statement types.
const (
	StatementTypeQuery       StatementType = iota // SELECT, SHOW, DESCRIBE
	StatementTypeDML                              // INSERT, UPDATE, DELETE
	StatementTypeDDLCreate                        // CREATE TABLE, CREATE DATABASE, etc.
	StatementTypeDDLDrop                          // DROP TABLE, DROP DATABASE, etc.
	StatementTypeDDLAlter                         // ALTER TABLE, etc.
	StatementTypeCopy                             // COPY INTO
	StatementTypeMerge                            // MERGE INTO
	StatementTypeTransaction                      // BEGIN, COMMIT, ROLLBACK
	StatementTypeOther                            // Unknown or unsupported
)

// Classifier provides SQL statement classification functionality.
type Classifier struct{}

// NewClassifier creates a new SQL classifier.
func NewClassifier() *Classifier {
	return &Classifier{}
}

// ClassifyResult contains the classification result of a SQL statement.
type ClassifyResult struct {
	Type            StatementType
	StatementTypeID config.StatementTypeID
	IsQuery         bool
	IsDDL           bool
	IsDML           bool
}

// leadingSQL returns sql uppercased and stripped of the whitespace and leading
// comments that precede the statement keyword.
//
// Classification is prefix-based, so without this a statement introduced by a
// comment is misread: "-- note\nSELECT 1" does not start with SELECT, is
// classified as DML, and its result set is silently discarded.
func leadingSQL(sql string) string {
	return strings.ToUpper(trimLeadingComments(sql))
}

// trimLeadingComments removes whitespace, "--" line comments and "/* */" block
// comments from the front of sql. Block comments nest, as they do in Snowflake
// and DuckDB. An unterminated block comment leaves nothing to classify.
func trimLeadingComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)

		switch {
		case strings.HasPrefix(sql, "--"):
			end := strings.IndexByte(sql, '\n')
			if end < 0 {
				return ""
			}
			sql = sql[end+1:]

		case strings.HasPrefix(sql, "/*"):
			rest, ok := skipBlockComment(sql)
			if !ok {
				return ""
			}
			sql = rest

		default:
			return sql
		}
	}
}

// skipBlockComment consumes one nested block comment from the front of sql and
// returns what follows. ok is false when the comment is never closed.
func skipBlockComment(sql string) (rest string, ok bool) {
	depth := 0
	for i := 0; i+1 < len(sql); i++ {
		switch {
		case sql[i] == '/' && sql[i+1] == '*':
			depth++
			i++
		case sql[i] == '*' && sql[i+1] == '/':
			depth--
			i++
			if depth == 0 {
				return sql[i+1:], true
			}
		}
	}
	return "", false
}

// Classify analyzes a SQL statement and returns its classification.
func (c *Classifier) Classify(sql string) ClassifyResult {
	upperSQL := leadingSQL(sql)

	// Check for query statements
	if c.isQueryStatement(upperSQL) {
		return ClassifyResult{
			Type:            StatementTypeQuery,
			StatementTypeID: config.StatementTypeSelect,
			IsQuery:         true,
			IsDDL:           false,
			IsDML:           false,
		}
	}

	// Check for DDL statements
	if strings.HasPrefix(upperSQL, "CREATE") {
		return ClassifyResult{
			Type:            StatementTypeDDLCreate,
			StatementTypeID: config.StatementTypeDDL,
			IsQuery:         false,
			IsDDL:           true,
			IsDML:           false,
		}
	}

	if strings.HasPrefix(upperSQL, "DROP") {
		return ClassifyResult{
			Type:            StatementTypeDDLDrop,
			StatementTypeID: config.StatementTypeDrop,
			IsQuery:         false,
			IsDDL:           true,
			IsDML:           false,
		}
	}

	if strings.HasPrefix(upperSQL, "ALTER") {
		return ClassifyResult{
			Type:            StatementTypeDDLAlter,
			StatementTypeID: config.StatementTypeDDL,
			IsQuery:         false,
			IsDDL:           true,
			IsDML:           false,
		}
	}

	// Check for COPY INTO statement
	if strings.HasPrefix(upperSQL, "COPY") {
		return ClassifyResult{
			Type:            StatementTypeCopy,
			StatementTypeID: config.StatementTypeDML, // COPY is treated as DML
			IsQuery:         false,
			IsDDL:           false,
			IsDML:           true,
		}
	}

	// Check for MERGE statement
	if strings.HasPrefix(upperSQL, "MERGE") {
		return ClassifyResult{
			Type:            StatementTypeMerge,
			StatementTypeID: config.StatementTypeDML, // MERGE is treated as DML
			IsQuery:         false,
			IsDDL:           false,
			IsDML:           true,
		}
	}

	// Check for transaction control statements
	if c.isTransactionStatement(upperSQL) {
		return ClassifyResult{
			Type:            StatementTypeTransaction,
			StatementTypeID: config.StatementTypeDML, // Transaction control statements
			IsQuery:         false,
			IsDDL:           false,
			IsDML:           false,
		}
	}

	// Default to DML for INSERT, UPDATE, DELETE, etc.
	return ClassifyResult{
		Type:            StatementTypeDML,
		StatementTypeID: config.StatementTypeDML,
		IsQuery:         false,
		IsDDL:           false,
		IsDML:           true,
	}
}

// isQueryStatement checks if the SQL is a query (read-only) statement.
func (c *Classifier) isQueryStatement(upperSQL string) bool {
	return strings.HasPrefix(upperSQL, "SELECT") ||
		strings.HasPrefix(upperSQL, "CALL") ||
		strings.HasPrefix(upperSQL, "LIST") ||
		strings.HasPrefix(upperSQL, "SHOW") ||
		strings.HasPrefix(upperSQL, "DESCRIBE") ||
		strings.HasPrefix(upperSQL, "DESC") ||
		strings.HasPrefix(upperSQL, "EXPLAIN")
}

// IsCreateStage checks if the SQL creates a named stage.
func (c *Classifier) IsCreateStage(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "CREATE STAGE") ||
		strings.HasPrefix(upperSQL, "CREATE OR REPLACE STAGE")
}

// IsDropStage checks if the SQL drops a named stage.
func (c *Classifier) IsDropStage(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "DROP STAGE")
}

// IsListStage checks if the SQL lists files in a named stage.
func (c *Classifier) IsListStage(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "LIST @")
}

// IsShowStages checks if the SQL lists named stages.
func (c *Classifier) IsShowStages(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "SHOW STAGES")
}

// IsCreateProcedure checks if the SQL creates a stored procedure.
func (c *Classifier) IsCreateProcedure(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "CREATE PROCEDURE") ||
		strings.HasPrefix(upperSQL, "CREATE OR REPLACE PROCEDURE")
}

// IsDropProcedure checks if the SQL drops a stored procedure.
func (c *Classifier) IsDropProcedure(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "DROP PROCEDURE")
}

// IsCall checks if the SQL calls a stored procedure.
func (c *Classifier) IsCall(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "CALL")
}

// IsShowProcedures checks if the SQL lists stored procedures.
func (c *Classifier) IsShowProcedures(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "SHOW PROCEDURES")
}

// IsCreateStream checks if the SQL creates a stream.
func (c *Classifier) IsCreateStream(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "CREATE STREAM") ||
		strings.HasPrefix(upperSQL, "CREATE OR REPLACE STREAM")
}

// IsDropStream checks if the SQL drops a stream.
func (c *Classifier) IsDropStream(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "DROP STREAM")
}

// IsShowStreams checks if the SQL lists streams.
func (c *Classifier) IsShowStreams(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "SHOW STREAMS")
}

func (c *Classifier) IsCreateTask(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "CREATE TASK") || strings.HasPrefix(upperSQL, "CREATE OR REPLACE TASK")
}

func (c *Classifier) IsAlterTask(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "ALTER TASK")
}

func (c *Classifier) IsDropTask(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "DROP TASK")
}

func (c *Classifier) IsExecuteTask(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "EXECUTE TASK")
}

func (c *Classifier) IsShowTasks(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "SHOW TASKS")
}

// isTransactionStatement checks if the SQL is a transaction control statement.
func (c *Classifier) isTransactionStatement(upperSQL string) bool {
	return strings.HasPrefix(upperSQL, "BEGIN") ||
		strings.HasPrefix(upperSQL, "START TRANSACTION") ||
		strings.HasPrefix(upperSQL, "COMMIT") ||
		strings.HasPrefix(upperSQL, "ROLLBACK")
}

// IsCreateTable checks if the SQL is a CREATE TABLE statement.
func (c *Classifier) IsCreateTable(sql string) bool {
	upperSQL := leadingSQL(sql)
	prefixes := []string{
		"CREATE TABLE",
		"CREATE TEMP TABLE",
		"CREATE TEMPORARY TABLE",
		"CREATE TRANSIENT TABLE",
		"CREATE OR REPLACE TABLE",
		"CREATE OR REPLACE TEMP TABLE",
		"CREATE OR REPLACE TEMPORARY TABLE",
		"CREATE OR REPLACE TRANSIENT TABLE",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(upperSQL, prefix) {
			return true
		}
	}
	return false
}

// IsCreateSchema checks if the SQL creates a logical Snowflake schema.
func (c *Classifier) IsCreateSchema(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "CREATE SCHEMA") ||
		strings.HasPrefix(upperSQL, "CREATE OR REPLACE SCHEMA")
}

// IsDropSchema checks if the SQL drops a logical Snowflake schema.
func (c *Classifier) IsDropSchema(sql string) bool {
	return strings.HasPrefix(leadingSQL(sql), "DROP SCHEMA")
}

// IsDropTable checks if the SQL is a DROP TABLE statement.
func (c *Classifier) IsDropTable(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "DROP TABLE")
}

// IsCopy checks if the SQL is a COPY INTO statement.
func (c *Classifier) IsCopy(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "COPY")
}

// DefaultClassifier is the default SQL classifier instance.
var DefaultClassifier = NewClassifier()

// ClassifySQL is a convenience function using the default classifier.
func ClassifySQL(sql string) ClassifyResult {
	return DefaultClassifier.Classify(sql)
}

// IsQuery is a convenience function to check if SQL is a query.
func IsQuery(sql string) bool {
	return DefaultClassifier.Classify(sql).IsQuery
}

// IsDDL is a convenience function to check if SQL is a DDL statement.
func IsDDL(sql string) bool {
	return DefaultClassifier.Classify(sql).IsDDL
}

// GetStatementTypeID is a convenience function to get the statement type ID.
func GetStatementTypeID(sql string) config.StatementTypeID {
	return DefaultClassifier.Classify(sql).StatementTypeID
}

// IsCopy is a convenience function to check if SQL is a COPY statement.
func IsCopy(sql string) bool {
	return DefaultClassifier.IsCopy(sql)
}

// IsMerge checks if the SQL is a MERGE INTO statement.
func (c *Classifier) IsMerge(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "MERGE")
}

// IsTransaction checks if the SQL is a transaction control statement.
func (c *Classifier) IsTransaction(sql string) bool {
	upperSQL := leadingSQL(sql)
	return c.isTransactionStatement(upperSQL)
}

// IsMerge is a convenience function to check if SQL is a MERGE statement.
func IsMerge(sql string) bool {
	return DefaultClassifier.IsMerge(sql)
}

// IsTransaction is a convenience function to check if SQL is a transaction statement.
func IsTransaction(sql string) bool {
	return DefaultClassifier.IsTransaction(sql)
}

// IsBegin checks if the SQL is a BEGIN/START TRANSACTION statement.
func IsBegin(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "BEGIN") || strings.HasPrefix(upperSQL, "START TRANSACTION")
}

// IsCommit checks if the SQL is a COMMIT statement.
func IsCommit(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "COMMIT")
}

// IsRollback checks if the SQL is a ROLLBACK statement.
func IsRollback(sql string) bool {
	upperSQL := leadingSQL(sql)
	return strings.HasPrefix(upperSQL, "ROLLBACK")
}
