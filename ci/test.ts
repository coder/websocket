#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import cp from "child_process"
import * as events from "events"
import * as readline from "readline"
import replaceInFile from "replace-in-file"
import { exec, main, selectCtx, spawn, wasmEnv } from "./lib"

if (require.main === module) {
  main(test)
}

export async function test(ctx: Promise<unknown>) {
  const args = ["-parallel=1024", "-coverprofile=ci/out/coverage.prof", "-coverpkg=./..."]

  if (process.env.CI) {
    args.push("-race")
  }

  const cliArgs = process.argv.splice(2)
  if (cliArgs.length > 0) {
    args.push(...cliArgs)
  } else {
    args.push("./...")
  }

  const p1 = spawn(ctx, "go", ["test", ...args], {
    stdio: "inherit",
  })
  const p2 = wasmTest(ctx)
  await Promise.all([p1, p2])

  // Depending on the code tested, we may not have replaced anything so we do not
  // check whether anything was replaced.
  await selectCtx(
    ctx,
    replaceInFile({
      files: "./ci/out/coverage.prof",
      from: [/.+frame_stringer.go.+\n/g, /.+wsjstest\/.+\n/g, /.+wsecho\/.+\n/g, /.+assert\/.+\n/g],
      to: "",
    }),
  )

  let p: Promise<unknown> = exec(ctx, "go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html")

  if (process.env.CI) {
    p = Promise.all([p, codecov(ctx)])
  }

  await p
}

async function wasmTest(ctx: Promise<unknown>) {
  await Promise.all([
    exec(ctx, "go install ./internal/wsjstest"),
    exec(ctx, "go install github.com/agnivade/wasmbrowsertest"),
  ])

  const url = await startWasmTestServer(ctx)

  await exec(ctx, "go test -exec=wasmbrowsertest ./...", {
    env: {
      ...wasmEnv,
      WS_ECHO_SERVER_URL: url,
    },
  })
}

async function startWasmTestServer(ctx: Promise<unknown>): Promise<string> {
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

function codecov(ctx: Promise<unknown>) {
  return exec(ctx, "curl -s https://codecov.io/bash | bash -s -- -Z -f ci/out/coverage.prof")
}
