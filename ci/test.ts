#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import * as https from "https"
import replaceInFile from "replace-in-file"
import { exec, main, selectCtx, spawn } from "./lib"

if (process.argv[1] === __filename) {
  main(test)
}

export async function test(ctx: Promise<unknown>) {
  const args = [
    "-parallel=1024",
    "-coverprofile=ci/out/coverage.prof",
    "-coverpkg=./...",
  ]

  if (process.env.CI) {
    args.push("-race")
  }

  const cliArgs = process.argv.splice(2)
  if (cliArgs.length > 0) {
    args.push(...cliArgs)
  } else {
    args.push("./...")
  }

  await spawn(ctx, "go", ["test", ...args], {
    timeout: 60_000,
    stdio: "inherit",
  })

  // Depending on the code tested, we may not have replaced anything so we do not
  // check whether anything was replaced.
  await selectCtx(ctx, replaceInFile({
    files: "./ci/out/coverage.prof",
    from: [
      /.+frame_stringer.go:.+\n/g,
      /.+wsjstest:.+\n/g,
      /.+wsecho:.+\n/g,
    ],
    to: "",
  }))

  let p: Promise<unknown> = exec(ctx, "go tool cover -html=ci/out/coverage.prof -o=ci/out/coverage.html")

  if (process.env.CI) {
    const script = https.get("https://codecov.io/bash")
    const p2 = spawn(ctx, "bash -Z -R . -f ci/out/coverage.prof", [], {
      stdio: [script],
    })
    p = Promise.all([p, p2])
  }

  await p
}
