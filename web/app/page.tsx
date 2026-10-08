import { authenticated } from "../lib/auth";
import Dashboard from "./dashboard";

export const dynamic = "force-dynamic";

export default async function Home() {
  return <Dashboard initialAuthenticated={await authenticated()} />;
}
