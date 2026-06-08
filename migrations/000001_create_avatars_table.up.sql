CREATE TABLE IF NOT EXISTS avatars (
    id uuid PRIMARY KEY,
    user_id text NOT NULL,
    file_name text NOT NULL,
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    width integer NULL CHECK (width IS NULL OR width > 0),
    height integer NULL CHECK (height IS NULL OR height > 0),
    status text NOT NULL,
    original_object_key text NOT NULL,
    thumb_100_object_key text NULL,
    thumb_300_object_key text NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    deleted_at timestamptz NULL,
    CONSTRAINT avatars_status_check CHECK (
        status IN ('processing', 'ready', 'failed', 'deleting', 'deleted')
    )
);

COMMENT ON TABLE avatars IS 'Аватарки пользователей и состояние обработки файлов';
COMMENT ON COLUMN avatars.id IS 'Уникальный идентификатор аватарки';
COMMENT ON COLUMN avatars.user_id IS 'Email владельца аватарки из X-User-ID';
COMMENT ON COLUMN avatars.file_name IS 'Исходное имя загруженного файла';
COMMENT ON COLUMN avatars.mime_type IS 'Проверенный MIME-тип изображения';
COMMENT ON COLUMN avatars.size_bytes IS 'Размер оригинального файла в байтах';
COMMENT ON COLUMN avatars.width IS 'Ширина оригинального изображения в пикселях';
COMMENT ON COLUMN avatars.height IS 'Высота оригинального изображения в пикселях';
COMMENT ON COLUMN avatars.status IS 'Текущий статус жизненного цикла аватарки';
COMMENT ON COLUMN avatars.original_object_key IS 'S3 object key оригинального изображения';
COMMENT ON COLUMN avatars.thumb_100_object_key IS 'S3 object key миниатюры 100x100';
COMMENT ON COLUMN avatars.thumb_300_object_key IS 'S3 object key миниатюры 300x300';
COMMENT ON COLUMN avatars.created_at IS 'Время создания записи';
COMMENT ON COLUMN avatars.updated_at IS 'Время последнего обновления записи';
COMMENT ON COLUMN avatars.deleted_at IS 'Время мягкого удаления записи';

CREATE INDEX IF NOT EXISTS idx_avatars_user_id_created_at
    ON avatars (user_id, created_at DESC);

COMMENT ON INDEX idx_avatars_user_id_created_at IS 'Ускоряет получение аватарок пользователя по времени создания';

CREATE INDEX IF NOT EXISTS idx_avatars_user_id_active
    ON avatars (user_id)
    WHERE deleted_at IS NULL;

COMMENT ON INDEX idx_avatars_user_id_active IS 'Ускоряет выборку активных аватарок пользователя';

CREATE INDEX IF NOT EXISTS idx_avatars_status
    ON avatars (status);

COMMENT ON INDEX idx_avatars_status IS 'Ускоряет операционные запросы worker по статусу';
