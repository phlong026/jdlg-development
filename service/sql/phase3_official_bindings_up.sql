CREATE TABLE IF NOT EXISTS official_account_binding_intents (
    state_hash CHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    consumed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (state_hash),
    KEY idx_official_binding_intent_user (user_id, created_at),
    KEY idx_official_binding_intent_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS official_account_bindings (
    app_id VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    open_id VARCHAR(128) NOT NULL,
    union_id VARCHAR(128) NOT NULL DEFAULT '',
    version BIGINT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    subscribed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (app_id, user_id),
    UNIQUE KEY uk_official_binding_openid (app_id, open_id),
    KEY idx_official_binding_subscription (app_id, subscribed, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS official_account_callback_events (
    app_id VARCHAR(64) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    open_id VARCHAR(128) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (app_id, event_key),
    KEY idx_official_callback_openid (app_id, open_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
