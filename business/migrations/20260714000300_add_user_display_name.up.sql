ALTER TABLE business.user_account
    ADD COLUMN display_name varchar(160) NOT NULL DEFAULT '用户';

ALTER TABLE business.user_account
    ADD CONSTRAINT ck_user_account__display_name
    CHECK (
        length(btrim(display_name)) BETWEEN 1 AND 160
        AND display_name !~ '[[:cntrl:]]'
    );

COMMENT ON COLUMN business.user_account.display_name
    IS '用户安全展示名，用于认证会话和工作台展示，不包含密码或令牌信息';

ALTER TABLE business.user_account
    ALTER COLUMN display_name DROP DEFAULT;
