package skills

import "errors"

var (
	errQueryTooShort = errors.New("err.skills.query_too_short")
	errRateLimited   = errors.New("err.skills.rate_limited")
	errSearchFailed  = errors.New("err.skills.search_failed")
	errDownloadFail  = errors.New("err.skills.download_failed")
	errInvalidSkill  = errors.New("err.skills.invalid_skill")
	errInvalidID     = errors.New("err.skills.invalid_id")
	errTargetConf    = errors.New("err.skills.target_conflict")
	errNoTargets     = errors.New("err.skills.no_targets")
)
