package apierror

import (
	commondtov1 "github.com/dangngochoainam/gm-core/application/common/v1/dto"
)

// SystemCode returns the number gm-core registers for reason, or
// httpStatus*100 when the reason is empty, unknown, or SYSTEM_CODE_UNSPECIFIED.
func SystemCode(reason string, httpStatus int) int32 {
	if code := commondtov1.SystemCode_value[reason]; code != 0 {
		return code
	}
	return int32(httpStatus) * 100
}
