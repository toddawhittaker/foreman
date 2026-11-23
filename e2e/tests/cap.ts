// The per-test time cap, in milliseconds: playwright.config.ts sets it as the
// timeout, and the fixture in foreman.ts fails a test that raises it untagged.
export const capMs = 30_000;
