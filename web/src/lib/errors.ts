// Turns API error text into something a site manager can act on.
//
// Gin's validator returns raw text like
//   Key: 'InviteRequest.Mobile' Error:Field validation for 'Mobile' failed on the 'e164' tag
// which is fine for developers but not for the dashboard.

const FIELD_LABELS: Record<string, string> = {
  CompanyName: "Company name",
  FullName: "Full name",
  SiteName: "First site name",
  RefreshToken: "Refresh token",
  ParentID: "Parent location",
  OrgID: "Organisation",
};

function label(field: string) {
  return FIELD_LABELS[field] ?? field.replace(/([a-z])([A-Z])/g, "$1 $2");
}

function describe(field: string, tag: string) {
  const name = label(field);
  switch (tag) {
    case "required":
      return `${name} is required.`;
    case "email":
      return `${name} must be a valid email address.`;
    case "e164":
      return `${name} must be in international format, e.g. +27821234567.`;
    case "min":
      return field === "Password"
        ? "Password must be at least 10 characters."
        : `${name} is too short.`;
    case "max":
      return `${name} is too long.`;
    case "oneof":
      return `${name} has an unknown value.`;
    case "uuid":
      return `${name} is not valid.`;
    default:
      return `${name} is not valid.`;
  }
}

export function friendlyMessage(status: number, raw: string): string {
  const fieldErrors = [
    ...raw.matchAll(/validation for '(\w+)' failed on the '(\w+)' tag/g),
  ];
  if (fieldErrors.length > 0) {
    return fieldErrors.map(([, field, tag]) => describe(field, tag)).join(" ");
  }

  if (status === 403) {
    return "Your role doesn't allow this. Ask an owner or SHE officer.";
  }
  if (raw === "org_id is required for this account") {
    return "This account belongs to more than one company. Signing in to a specific company isn't available on the dashboard yet.";
  }
  if (raw === "internal error" || !raw) {
    return "Something went wrong on the server. Try again.";
  }
  // The API's own messages are already written for people.
  return raw.charAt(0).toUpperCase() + raw.slice(1) + (raw.endsWith(".") ? "" : ".");
}
