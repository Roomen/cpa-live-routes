# UI regression tests

Run with Node.js 22 or newer:

```sh
npm ci
npm test
npx playwright install chromium
npm run test:browser
PLAYWRIGHT_MODULE="$PWD/node_modules/playwright" npm run test:i18n
PLAYWRIGHT_MODULE="$PWD/node_modules/playwright" npm run test:regression
```

`npm test` runs the production path functions in a Node VM. It checks every
segment boundary in both traversal directions, the queue and credential resting
ports, session expansion, and sessionless requests. Removing the LTR session
bridge reproduces the original discontinuity.

The browser suite serves the real page on an ephemeral loopback port. It injects
a fixture into the script closure only in the served test response; the shipped
page exposes no new testing API. Fixed synthetic clients, sessions, credentials
and model names replace demo traffic. No CPA server, management key or remote
requests are used. Screenshots and a result summary go to ignored `out/`.

The 80-case matrix covers two directions, five session shapes (expanded,
collapsed, absent, child sessions and folded children), two layout preferences,
two viewport widths and two themes. Below 900px split intentionally falls back
to tabs. Other checks cover actual canvas clicks, flight state transitions,
preserving flights during direction changes, unchanged timeline pixels/state,
reload persistence, language-sensitive accessibility labels and blocked storage. The i18n suite adds
54 initial-language/language/direction/view combinations under the production CSP.
The regression suite retains the existing authentication, history, idle and layout
checks. Browser startup failures close their test servers before exiting.
Split mode retains upstream's static flow behavior; it does not enable flights.

To use an existing Chromium installation, set
`PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` to its executable path. For reproducible
visual comparisons of upstream and the feature, run:

```sh
node web/test/capture.cjs
```

An optional first argument supplies another local HTML file as the pre-change
comparison (for example an uncommitted customization). Its content is never
copied into the repository. All captures still use the synthetic fixture.
