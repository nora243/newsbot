import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = { title: "Newsbot · Phòng biên tập", description: "Quản lý nguồn tin, sản xuất và duyệt video Newsbot." };

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return <html lang="vi"><body>{children}</body></html>;
}
