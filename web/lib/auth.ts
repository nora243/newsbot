import { cookies } from "next/headers";
import { SESSION_COOKIE, validSession } from "./session.mjs";

export async function authenticated() {
  const jar = await cookies();
  return validSession(jar.get(SESSION_COOKIE)?.value, process.env.ADMIN_TOKEN);
}
