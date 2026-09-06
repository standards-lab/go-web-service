CREATE TABLE organization (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  parent_id uuid REFERENCES organization (id),
  code text NOT NULL,
  name text NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT uq_organization_parent_code UNIQUE NULLS NOT DISTINCT (parent_id, code),
  CONSTRAINT cc_organization_code CHECK (code ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
  CONSTRAINT cc_organization_name CHECK (name <> ''),
  CONSTRAINT cc_organization_parent_not_self CHECK (parent_id <> id)
);
