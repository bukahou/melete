import { defineConfig } from "vitest/config";

// 只测纯逻辑（*.test.ts）；不起 Next、不碰 DOM。
// session-core.ts 之所以拆出来不 import Next，就是为了能在这里裸跑。
export default defineConfig({
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
