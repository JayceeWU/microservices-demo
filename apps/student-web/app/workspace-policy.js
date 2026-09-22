export function activeStudioOptions(memberships) {
  const byStudio = new Map();
  for (const membership of memberships ?? []) {
    if (!membership.active || !membership.studioId || byStudio.has(membership.studioId)) continue;
    byStudio.set(membership.studioId, {
      studioId: membership.studioId,
      studioName: membership.studioName || membership.studioId,
    });
  }
  return [...byStudio.values()];
}

export function defaultStudio(options, currentStudioId) {
  return (
    options.find((option) => option.studioId === currentStudioId)?.studioId ??
    options[0]?.studioId ??
    ''
  );
}
