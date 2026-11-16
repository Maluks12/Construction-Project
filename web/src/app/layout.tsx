import type { Metadata } from "next";
import "@fontsource-variable/inter";
import "@fontsource-variable/space-grotesk";

import "./globals.css";

export const metadata: Metadata = {
  title: { default: "SiteProof", template: "%s · SiteProof" },
  description: "Site safety and compliance for South African construction.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en-ZA" className="h-full antialiased">
      <body className="min-h-full font-sans">{children}</body>
    </html>
  );
}
