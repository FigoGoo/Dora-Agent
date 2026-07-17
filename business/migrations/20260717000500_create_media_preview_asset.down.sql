DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM business.media_preview_finalization_receipt LIMIT 1)
        OR EXISTS (SELECT 1 FROM business.media_preview_preparation_receipt LIMIT 1)
        OR EXISTS (SELECT 1 FROM business.media_preview_asset LIMIT 1) THEN
        RAISE EXCEPTION '媒体预览表仍包含业务数据，拒绝自动回滚'
            USING ERRCODE = '55000';
    END IF;
END
$$;

DROP TABLE business.media_preview_finalization_receipt;
DROP TABLE business.media_preview_preparation_receipt;
DROP TABLE business.media_preview_asset;
