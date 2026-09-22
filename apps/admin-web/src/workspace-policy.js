export function resolveAdminWorkspace(membershipResponse, platformStudios, currentStudioId) {
  const memberships = membershipResponse?.memberships ?? [];
  const allowed = memberships.filter(
    (membership) => membership.active && membership.role === 'studio_admin' && membership.studioId,
  );
  const platformAdmin = (membershipResponse?.globalRoles ?? []).includes('platform_admin');
  const studioOptions = platformAdmin
    ? (platformStudios ?? []).map((studio) => ({
        studioId: studio.id,
        studioName: studio.name,
      }))
    : allowed;
  const defaultStudioId =
    studioOptions.find((membership) => membership.studioId === currentStudioId)?.studioId ??
    studioOptions.find((membership) => membership.studioId)?.studioId ??
    '';
  return { allowed, platformAdmin, studioOptions, defaultStudioId };
}
