package core

import "fmt"

type NoFreeQuotaError struct {
	Task     TaskType `json:"task"`
	Provider []string `json:"providers"`
}

func (e NoFreeQuotaError) Error() string {
	return fmt.Sprintf("no_free_quota: no free provider available for task %q", e.Task)
}

func IsNoFreeQuota(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(NoFreeQuotaError)
	if ok {
		return true
	}
	_, ok = err.(*NoFreeQuotaError)
	return ok
}
