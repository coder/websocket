#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import cp from "child_process"
import * as events from "events"
import * as readline from "readline"
import { exec, main, selectCtx } from "./lib"

if (process.argv[1] === __filename) {
  main(wasm)
}

const wasmEnv = {
  ...process.env,
  GOOS: "js",
  GOARCH: "wasm",
}

export async function wasm(ctx: Promise<unknown>) {
  await Promise.all([
      exec(ctx, "go vet ./...", {
        env: wasmEnv,
      }),
      goLint(ctx),
      wasmTest(ctx),
    ],
  )
}

async function goLint(ctx: Promise<unknown>) {
  await exec(ctx, "go install golang.org/x/lint/golint")
  await exec(ctx, "golint -set_exit_status ./...", {
    env: wasmEnv,
  })
}

async function wasmTest(ctx: Promise<unknown>) {
  await Promise.all([
    exec(ctx, "go install ./internal/wsjstest"),
    exec(ctx, "go install github.com/agnivade/wasmbrowsertest"),
  ])

  const url = await startServer(ctx)

  await exec(ctx, "go test -exec=wasmbrowsertest ./...", {
    env: {
      ...wasmEnv,
      WS_ECHO_SERVER_URL: url,
    },
  })
}

async function startServer(ctx: Promise<unknown>): Promise<string> {
  const wsjstest = cp.spawn("wsjstest")
  ctx.finally(wsjstest.kill.bind(wsjstest))

  const rl = readline.createInterface({
    input: wsjstest.stdout!,
  })

  try {
    const p = events.once(rl, "line")
    const a = await selectCtx(ctx, p)
    return a[0]
  } finally {
    rl.close()
  }
}
