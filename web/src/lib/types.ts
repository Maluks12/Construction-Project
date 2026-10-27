// Shapes returned by the Go API (construction_db). Keep these in step with the
// json tags in internal/core/*/model.go.

export type TokenPair = {
  access_token: string;
  refresh_token: string;
  expires_in: number; // seconds
  token_type: "Bearer";
};

export type Me = {
  user_id: string;
  org_id: string;
  session_id: string;
};

export type Role =
  | "owner"
  | "she_officer"
  | "supervisor"
  | "claims_handler"
  | "contracts_manager"
  | "bookkeeper";

export type SiteStatus = "active" | "completed" | "archived";

export type Site = {
  id: string;
  org_id: string;
  name: string;
  address: string | null;
  status: SiteStatus;
  created_at: string;
};

export type LocationKind = "zone" | "floor";

export type Location = {
  id: string;
  site_id: string;
  parent_id: string | null;
  kind: LocationKind;
  name: string;
  created_at: string;
};

export type SiteMember = {
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
};

export type PendingInvite = {
  id: string;
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
  created_at: string;
  expires_at: string;
};

export type InviteResponse = {
  id: string;
  user_id: string;
  token: string;
  expires_at: string;
};

export type InvitePreview = {
  org_name: string;
  full_name: string;
  role: Role;
  has_password: boolean;
  expires_at: string;
};

export type Session = {
  id: string;
  device_name: string | null;
  user_agent: string | null;
  created_at: string;
  last_used_at: string;
  current: boolean;
};
