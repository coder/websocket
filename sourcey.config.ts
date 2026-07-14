import { defineConfig, godoc } from "sourcey";

export default defineConfig({
  name: "websocket",
  siteUrl: "https://coder.github.io",
  baseUrl: "/websocket",
  repo: "https://github.com/coder/websocket",
  editBranch: "master",
  theme: {
    preset: "default",
    colors: {
      primary: "#7c3aed",
      light: "#8b5cf6",
      dark: "#5b21b6",
    },
  },
  navigation: {
    tabs: [
      {
        tab: "Go API",
        slug: "",
        source: godoc({
          module: ".",
          packages: [".", "./wsjson"],
          mode: "live",
          includeTests: true,
          sourceBasePath: "",
        }),
      },
    ],
  },
});
