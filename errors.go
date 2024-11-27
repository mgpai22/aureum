package aureum

import "fmt"

type EvaluationError struct {
	EvalError EvalError
}

func (e *EvaluationError) Error() string {
	return fmt.Sprintf("Evaluation failed: %s", e.EvalError.ErrorType)
}
