package peers

type Response struct {
	IsHealthy bool
	Reason    reason
}

type reason string

const (
	HealthyBecauseCRNotFound                           reason = "CR Not found, node is considered healthy"
	HealthyBecauseErrorsThresholdNotReached            reason = "Errors number hasn't reached threshold not querying peers yet, node is considered healthy"
	HealthyBecauseNoPeersResponseNotReachedMaxAttempts reason = "No response from peer hasn't passed the non responsive time threshold so still considered healthy"
	HealthyBecauseNoPeersWereFound                     reason = "No Peers where found, node is considered healthy"
	HealthyBecauseMostPeersCantAccessAPIServer         reason = "Most peers couldn't access API server, node is considered healthy"

	UnHealthyBecausePeersResponse  reason = "Node is reported unhealthy by it's peers"
	UnHealthyBecauseNodeIsIsolated reason = "Node is isolated, node is considered unhealthy"
)
