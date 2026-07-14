ALTER TABLE business.user_account
    DROP CONSTRAINT IF EXISTS ck_user_account__display_name;

ALTER TABLE business.user_account
    DROP COLUMN IF EXISTS display_name;
