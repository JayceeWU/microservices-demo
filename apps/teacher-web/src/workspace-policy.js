export function activeTeacherMemberships(memberships) {
  return (memberships ?? []).filter(
    (membership) =>
      membership.active && membership.role === 'teacher' && Boolean(membership.studioId),
  );
}

export function initialStudio(memberships, currentStudioId) {
  return (
    memberships.find((membership) => membership.studioId === currentStudioId)?.studioId ??
    memberships[0]?.studioId ??
    ''
  );
}
