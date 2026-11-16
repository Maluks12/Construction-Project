import type { Metadata } from "next";
import "@fontsource-variable/inter";
import "@fontsource-variable/space-grotesk";

import "./globals.css";

export const metadata: Metadata = {
  title: { default: "SiteProof Admin", template: "%s · SiteProof Admin" },
  description: "Internal portal for the SiteProof team.",
  // An internal tool: keep it out of search engines.
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en-ZA" className="h-full antialiased">
      <body className="min-h-full font-sans">{children}</body>
    </html>
  );
}
