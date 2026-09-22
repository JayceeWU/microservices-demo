// A lost response or a pending credit operation must replay the same command.
// Different form contents start a new command; a successful command is released.
export function createAttendanceCommands(api, newKey = () => crypto.randomUUID()) {
  const pending = new Map();
  return async (operation, value) => {
    const payload = JSON.stringify(
      Object.keys(value)
        .sort()
        .map((key) => [key, value[key]]),
    );
    let command = pending.get(operation);
    if (!command || command.payload !== payload) {
      command = { payload, key: newKey() };
      pending.set(operation, command);
    }
    let result;
    if (operation === 'walk')
      result = await api.addWalkIn(value.sessionId, value.studentId, value.reason, command.key);
    else if (operation === 'reverse')
      result = await api.reverseRedemption(value.bookingId, value.reason, command.key);
    else if (operation === 'correct')
      result = await api.correctAttendance(
        value.bookingId,
        value.attended === 'true',
        value.reason,
        command.key,
      );
    else throw new Error('Unknown attendance operation');
    if (pending.get(operation) === command) pending.delete(operation);
    return result;
  };
}
