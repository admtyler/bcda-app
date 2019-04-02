create table acos (
  uuid uuid not null unique primary key,
  cms_id char(5) null unique,
  name text not null,
  client_id text,
  alpha_secret text,
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone
);

create table users (
  uuid uuid not null primary key,
  name text not null,
  email text not null unique,
  aco_id uuid not null,
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone,
  foreign key (aco_id) references acos (uuid)
);

create table jobs (
  id serial not null primary key,
  aco_id uuid not null references acos,
  user_id uuid not null references users,
  request_url text not null,
  status text not null,
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone
);

create table tokens (
  uuid uuid not null primary key,
  user_id uuid not null references users,
  value text not null,
  active boolean not null default false,
  aco_id uuid,
  issued_at bigint,
  expires_on bigint,
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone
);

create table beneficiaries (
  id serial not null primary key,
  blue_button_id text not null,
  created_at timestamp with time zone not null default now(),
  updated_at timestamp with time zone not null default now(),
  deleted_at timestamp with time zone
);

create table acos_beneficiaries (
  aco_id uuid not null,
  beneficiary_id int not null
);