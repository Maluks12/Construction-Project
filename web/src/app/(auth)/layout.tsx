import { Logo } from "@/components/ui";

export default function AuthLayout({ children }: LayoutProps<"/">) {
  return (
    <main className="flex min-h-screen flex-col items-center px-4 py-12 sm:justify-center">
      <div className="w-full max-w-sm">
        <div className="mb-8">
          <Logo />
        </div>
        {children}
      </div>
    </main>
  );
}
