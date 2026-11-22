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
  experimental: {
    // Document uploads (certificates, letters, H&S file and vault versions)
    // go through Server Actions (src/lib/upload.ts). The API takes files up
    // to 20 MiB; the extra megabyte is the form's other fields and the
    // multipart overhead. Without these, actions stop at 1 MB, and proxy.ts,
    // which buffers every request body, would pass on only the first 10 MB
    // of a larger file without an error.
    serverActions: { bodySizeLimit: "21mb" },
    proxyClientMaxBodySize: "21mb",
  },
};

export default nextConfig;
