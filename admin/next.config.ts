import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Cache Components is off on purpose: every dashboard page reads the login
  // cookie and shows live compliance data, so pages render per request.
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
};

export default nextConfig;
