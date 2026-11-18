CREATE TABLE author_subscriptions(
 author_id TEXT PRIMARY KEY REFERENCES authors(id) ON DELETE CASCADE,
 data TEXT NOT NULL
);
CREATE TRIGGER backup_change_author_subscription_insert AFTER INSERT ON author_subscriptions BEGIN UPDATE backup_business_state SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER backup_change_author_subscription_update AFTER UPDATE ON author_subscriptions BEGIN UPDATE backup_business_state SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER backup_change_author_subscription_delete AFTER DELETE ON author_subscriptions BEGIN UPDATE backup_business_state SET revision=revision+1 WHERE id=1; END;
