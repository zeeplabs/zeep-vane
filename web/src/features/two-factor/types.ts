// Response shapes of the auth-2fa-totp endpoints the profile page consumes
// (design.md Data Models). Mirrored from the Go handler's enrollResponse /
// confirm2FAResponse so the components stay typed against the real contract.
export interface EnrollResponse {
  secret: string;
  otpauth_uri: string;
}

export interface ConfirmResponse {
  recovery_codes: string[];
}
