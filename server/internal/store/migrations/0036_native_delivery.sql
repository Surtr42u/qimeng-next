CREATE TABLE native_delivery_plans(
 id TEXT PRIMARY KEY, package_id TEXT NOT NULL UNIQUE, source_instance TEXT NOT NULL, target_instance TEXT NOT NULL,
 state TEXT NOT NULL, created_at TEXT NOT NULL, data TEXT NOT NULL, payload BLOB
);
CREATE INDEX native_delivery_staged ON native_delivery_plans(state,created_at);
CREATE TABLE native_delivery_stage_requests(
 principal_id TEXT NOT NULL, request_key TEXT NOT NULL, input_hash TEXT NOT NULL,
 plan_id TEXT NOT NULL REFERENCES native_delivery_plans(id) ON DELETE CASCADE,
 PRIMARY KEY(principal_id,request_key)
);
CREATE TABLE native_delivery_bindings(
 source_instance TEXT NOT NULL, kind TEXT NOT NULL, object_id TEXT NOT NULL, block TEXT NOT NULL,
 source_revision INTEGER NOT NULL, payload_hash TEXT NOT NULL, target_id TEXT NOT NULL, target_revision INTEGER NOT NULL,
 PRIMARY KEY(source_instance,kind,object_id,block)
);
CREATE TABLE native_delivery_receipts(
 target_instance TEXT NOT NULL, source_instance TEXT NOT NULL, kind TEXT NOT NULL, object_id TEXT NOT NULL, block TEXT NOT NULL,
 source_revision INTEGER NOT NULL, payload_hash TEXT NOT NULL, data TEXT NOT NULL,
 PRIMARY KEY(target_instance,source_instance,kind,object_id,block,source_revision)
);
CREATE TRIGGER backup_native_delivery_commit AFTER UPDATE OF state ON native_delivery_plans WHEN NEW.state<>OLD.state BEGIN
 UPDATE backup_business_state SET revision=revision+1;
END;
