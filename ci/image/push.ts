#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import fs from "fs"
import { promisify } from "util"
import { main, spawn } from "../lib"

main(run, {
  timeout: 10 * 60_000,
})

async function run(ctx: Promise<unknown>) {
  await promisify(fs.copyFile)("./ci/image/dockerignore", ".dockerignore")

  try {
    await spawn(ctx, "docker build -f ./ci/image/Dockerfile -t nhooyr/websocket-ci .", [], {
      timeout: 180_000,
      stdio: "inherit",
    })
    await spawn(ctx, "docker push nhooyr/websocket-ci", [], {
      timeout: 30_000,
      stdio: "inherit",
    })
  } finally {
    await promisify(fs.unlink)(".dockerignore")
  }
}
