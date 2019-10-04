#!/usr/bin/env -S npx ts-node -P ci/tsconfig.json

import { fmt, gen } from "./fmt"
import { main } from "./lib"
import { lint } from "./lint"
import { test } from "./test"

main(run)

async function run(ctx: Promise<unknown>) {
  await gen(ctx)

  await Promise.all([
    fmt(ctx),
    lint(ctx),
    test(ctx),
  ])
}
