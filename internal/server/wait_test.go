package server_test

import "time"

// How long an asynchronous wait in this package's tests may take before it is
// called a failure.
//
// It was 2 seconds, hardcoded in five places, and it made these tests report
// "slow machine" as "broken code". TestProviderCachePurgeAPI reddened main
// with "refresh did not complete" while the same run logged
// `on-demand refresh completed provider=lg duration=3.204386771s` — the
// refresh completed fine, 1.2 seconds after the test stopped waiting for it.
//
// Every one of these waits polls and returns the instant its condition holds,
// so a generous ceiling costs nothing when the machine is quick and is the
// whole difference when it is not. None of them is asserting how FAST the
// system is; they are waiting for it to finish so the assertion after them —
// or TempDir cleanup — is not racing.
const asyncWait = 30 * time.Second
