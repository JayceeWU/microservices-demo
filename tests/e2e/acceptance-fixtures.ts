import { readFileSync } from 'node:fs';
import { join } from 'node:path';

type BrowserFixtures = {
  paginationSessionId: string;
  paginationBookingId: string;
  paginationBookingSessionId: string;
  payrollSessionId: string;
  payrollMonth: string;
  payrollAmountCents: number;
  attendanceSessionId: string;
  attendanceBookingId: string;
  attendanceMonth: string;
};

export function acceptanceFixtures(): BrowserFixtures | undefined {
  const directory = process.env.DANCEHUB_ACCEPTANCE_OUTPUT_DIR;
  return directory
    ? JSON.parse(readFileSync(join(directory, 'browser-fixtures.json'), 'utf8'))
    : undefined;
}
