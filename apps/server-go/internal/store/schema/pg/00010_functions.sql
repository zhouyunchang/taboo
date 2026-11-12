-- 不可变与完整性触发器函数。CREATE OR REPLACE：重复执行安全。
-- +goose Up


-- function: check_identity_scope

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION check_identity_scope()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM projects p JOIN machine_identities m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND m.id=NEW.identity_id) THEN
  RAISE EXCEPTION 'invalid resource ownership' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END
$function$;
-- +goose StatementEnd


-- function: check_workload_identity


-- +goose StatementBegin
CREATE OR REPLACE FUNCTION check_workload_identity()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
 BEGIN
 IF NOT EXISTS(SELECT 1 FROM machine_identities WHERE id=NEW.id AND org_id=NEW.org_id AND auth_type='external_oauth') THEN
 RAISE EXCEPTION 'external workload identity required' USING ERRCODE='23503';
 END IF; RETURN NEW;
END
$function$;
-- +goose StatementEnd


-- function: immutable_ownership


-- +goose StatementBegin
CREATE OR REPLACE FUNCTION immutable_ownership()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
 IF to_jsonb(NEW)->>'id' IS DISTINCT FROM to_jsonb(OLD)->>'id' OR to_jsonb(NEW)->>TG_ARGV[0] IS DISTINCT FROM to_jsonb(OLD)->>TG_ARGV[0] THEN
  RAISE EXCEPTION 'resource ownership is immutable' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END
$function$;
-- +goose StatementEnd

