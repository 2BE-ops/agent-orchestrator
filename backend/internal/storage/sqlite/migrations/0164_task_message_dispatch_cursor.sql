-- +goose Up
CREATE TABLE adaptive_task_message_dispatch_cursor (
    id INTEGER PRIMARY KEY CHECK(id=1),
    after_sequence INTEGER NOT NULL CHECK(after_sequence>=0)
);
INSERT INTO adaptive_task_message_dispatch_cursor(id,after_sequence) VALUES(1,0);
CREATE INDEX adaptive_task_message_delivery_recipient ON adaptive_task_message_deliveries(session_id,state);

-- +goose Down
DROP INDEX adaptive_task_message_delivery_recipient;
DROP TABLE adaptive_task_message_dispatch_cursor;
