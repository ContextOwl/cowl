package cli

const apiKeyPrefix = "cowl_pat_"

func apiKeyDisplayPrefix(token string) string {
	const n = len(apiKeyPrefix) + 6
	if len(token) < n {
		return token
	}
	return token[:n]
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
}
