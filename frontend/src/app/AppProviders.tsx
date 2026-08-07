import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { HashRouter } from "react-router";

export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            retry: (attempt, error) => attempt < 2 && !(error instanceof Error && error.name === "ApiError"),
            staleTime: 15_000,
            refetchOnWindowFocus: false
          }
        }
      })
  );

  return (
    <QueryClientProvider client={queryClient}>
      <HashRouter>{children}</HashRouter>
    </QueryClientProvider>
  );
}
