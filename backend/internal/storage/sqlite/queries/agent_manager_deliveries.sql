-- name: InsertAgentManagerDelivery :exec
INSERT INTO adaptive_agent_manager_deliveries(id,request_id,context_id,controller_id,number,session_id,owner,delivery_key,state,reason,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,'dispatching','Reserved before native delivery',?,?);

-- name: GetAgentManagerDelivery :one
SELECT * FROM adaptive_agent_manager_deliveries WHERE id=?;

-- name: ListAgentManagerDeliveries :many
SELECT * FROM adaptive_agent_manager_deliveries WHERE request_id=? ORDER BY number;

-- name: LatestAgentManagerRequestDelivery :one
SELECT * FROM adaptive_agent_manager_deliveries WHERE request_id=? ORDER BY number DESC LIMIT 1;

-- name: AgentManagerConversationBusy :one
SELECT EXISTS(SELECT 1 FROM adaptive_agent_manager_deliveries d WHERE d.controller_id=?
 AND (d.state IN ('dispatching','uncertain') OR (d.state='handed_off' AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions r WHERE r.request_id=d.request_id))));

-- name: ResolveAgentManagerDelivery :execrows
UPDATE adaptive_agent_manager_deliveries SET state=?,reason=?,updated_at=? WHERE id=? AND state='dispatching';

-- name: ListUnresolvedAgentManagerDeliveries :many
SELECT * FROM adaptive_agent_manager_deliveries WHERE state='dispatching' AND id>? ORDER BY id LIMIT ?;

-- name: ListDispatchableAgentManagerRequests :many
SELECT r.* FROM adaptive_agent_manager_requests r WHERE r.sequence>?
AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_request_resolutions x WHERE x.request_id=r.id)
AND NOT EXISTS(SELECT 1 FROM adaptive_agent_manager_deliveries d WHERE d.request_id=r.id AND d.state!='not_sent')
AND (SELECT count(*) FROM adaptive_agent_manager_deliveries d WHERE d.request_id=r.id)<4
ORDER BY r.sequence LIMIT ?;

-- name: AgentManagerDispatchCursor :one
SELECT after_sequence FROM adaptive_agent_manager_dispatch_cursor WHERE id=1;

-- name: SetAgentManagerDispatchCursor :execrows
UPDATE adaptive_agent_manager_dispatch_cursor SET after_sequence=? WHERE id=1;
