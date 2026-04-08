"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

const navItems = [
  { href: "/", label: "Dashboard", icon: "⌂" },
  { href: "/agents", label: "Agents", icon: "◉" },
  { href: "/skills", label: "Skills", icon: "⚡" },
  { href: "/crons", label: "Cron Jobs", icon: "⏱" },
  { href: "/admin", label: "Admin", icon: "⚙" },
  { href: "/admin/usage", label: "Usage", icon: "▤" },
  { href: "/admin/audit", label: "Audit Log", icon: "☰" },
];

export function Sidebar() {
  const pathname = usePathname();

  return (
    <aside className="flex h-screen w-56 flex-col border-r border-border bg-card">
      <div className="flex h-14 items-center border-b border-border px-4">
        <Link href="/" className="flex items-center gap-2">
          <span className="text-lg font-bold text-primary">CapyClaw</span>
        </Link>
      </div>

      <nav className="flex-1 space-y-1 p-3">
        {navItems.map((item) => {
          const isActive =
            pathname === item.href ||
            (item.href !== "/" && pathname.startsWith(item.href));
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-primary/10 text-primary font-medium"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground",
              )}
            >
              <span className="text-base">{item.icon}</span>
              {item.label}
            </Link>
          );
        })}
      </nav>

      <div className="border-t border-border p-3">
        <div className="text-xs text-muted-foreground">CapyClaw v0.1.0-alpha</div>
      </div>
    </aside>
  );
}
