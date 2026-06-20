ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS headers jsonb NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN outbox_events.headers IS 'Kafka headers включая W3C trace context';
