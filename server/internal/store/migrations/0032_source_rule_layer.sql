CREATE TABLE source_rule_settings (
    id INTEGER PRIMARY KEY CHECK(id=1),
    revision INTEGER NOT NULL,
    layer TEXT NOT NULL,
    engine_version INTEGER NOT NULL,
    run_state TEXT NOT NULL,
    cursor TEXT NOT NULL DEFAULT '',
    processed INTEGER NOT NULL DEFAULT 0,
    matched INTEGER NOT NULL DEFAULT 0,
    ambiguous INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT ''
);
INSERT INTO source_rule_settings(id,revision,layer,engine_version,run_state) VALUES(1,1,'{"groups":[],"stopWords":[]}',1,'pending');
CREATE INDEX sources_rule_name ON sources(name,id);
CREATE TRIGGER backup_source_rule_setting AFTER UPDATE OF layer ON source_rule_settings
WHEN OLD.layer IS NOT NEW.layer BEGIN UPDATE backup_business_state SET revision=revision+1; END;
