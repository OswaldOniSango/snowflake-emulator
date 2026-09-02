package query

import "fmt"

// Handler names the component that executes a statement. The emulator routes
// several statement kinds to dedicated processors before the translator sees
// them, so a preview that ignored this would show a translation that never runs.
type Handler string

// Components a statement can be routed to, in the order the executor checks.
const (
	HandlerTranslator  Handler = "translator"
	HandlerProcedure   Handler = "procedure_processor"
	HandlerStream      Handler = "stream_processor"
	HandlerTask        Handler = "task_processor"
	HandlerTransaction Handler = "transaction"
	HandlerCopy        Handler = "copy_processor"
	HandlerMerge       Handler = "merge_processor"
)

// TranslationPreview describes what a statement becomes on its way to DuckDB.
type TranslationPreview struct {
	Statement  string
	Translated string
	HandledBy  Handler

	// Complete reports whether Translated is the SQL DuckDB actually receives.
	// It is false when a processor rewrites, decomposes or interprets the
	// statement, in which case Translated shows only the base translation.
	Complete bool

	// Note explains, for a reader, why a preview is incomplete.
	Note string
}

// ResolveHandler reports which component executes sql. It mirrors the order in
// which Executor.QueryWithContext and Executor.ExecuteWithContext dispatch, so
// that the preview describes the path a statement really takes.
func ResolveHandler(sql string) Handler {
	classifier := NewClassifier()

	switch {
	case classifier.IsCall(sql),
		classifier.IsShowProcedures(sql),
		classifier.IsCreateProcedure(sql),
		classifier.IsDropProcedure(sql):
		return HandlerProcedure

	case classifier.IsShowStreams(sql),
		classifier.IsCreateStream(sql),
		classifier.IsDropStream(sql):
		return HandlerStream

	case classifier.IsShowTasks(sql),
		classifier.IsCreateTask(sql),
		classifier.IsAlterTask(sql),
		classifier.IsDropTask(sql),
		classifier.IsExecuteTask(sql):
		return HandlerTask

	case IsTransaction(sql):
		return HandlerTransaction

	case IsCopy(sql):
		return HandlerCopy

	case IsMerge(sql):
		return HandlerMerge

	default:
		return HandlerTranslator
	}
}

// handlerNotes explain what each processor does that a base translation cannot
// show. Only handlers that change the SQL before execution appear here.
var handlerNotes = map[Handler]string{
	HandlerProcedure: "The body is stored as metadata and interpreted statement by statement at CALL time, so it is never translated as a whole.",
	HandlerStream:    "Stream references are rewritten against the stream's recorded offset, which depends on catalog state.",
	HandlerTask:      "The task definition is stored and translated when the task runs, in its own database and schema context.",
	HandlerCopy:      "The stage reference is resolved to a file path on disk and the options are mapped to DuckDB's COPY, neither of which the base translation covers.",
	HandlerMerge:     "MERGE is attempted natively and otherwise decomposed into UPDATE, DELETE and INSERT, each translated separately.",
}

// PreviewTranslation returns the DuckDB SQL a statement translates to, without
// executing it.
//
// It reproduces the executor's own order — contextual table names first, then
// function translation — so the preview matches what execution would produce.
// Stream rewriting is left out because it reads catalog state; statements that
// depend on it are reported as incomplete instead.
func PreviewTranslation(sql string, executionContext ExecutionContext) (TranslationPreview, error) {
	if sql == "" {
		return TranslationPreview{}, fmt.Errorf("statement is required")
	}

	rewritten := rewriteContextualTableReferences(sql, executionContext)

	translated, err := NewTranslator().Translate(rewritten)
	if err != nil {
		return TranslationPreview{}, fmt.Errorf("translation error: %w", err)
	}

	handler := ResolveHandler(sql)
	note, incomplete := handlerNotes[handler]

	return TranslationPreview{
		Statement:  sql,
		Translated: translated,
		HandledBy:  handler,
		Complete:   !incomplete,
		Note:       note,
	}, nil
}
