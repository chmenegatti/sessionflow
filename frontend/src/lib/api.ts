// Única porta de entrada para os serviços Go (bindings gerados pelo Wails).
// Componentes não importam bindings diretamente.
export {
  AnalyticsService,
  AppService,
  CostService,
  PatternsService,
  ResumeService,
  SearchService,
  SessionService,
  SettingsService,
  StartupService,
  StatsService,
  SyncService,
  TranscriptService,
} from "../../bindings/sessionflow/internal/service";
export { CostsUpdated, IndexUpdated } from "../../bindings/events";
export type {
  NamedCount,
  Field,
  TokenSums,
  UsageGroup,
  UsageSessionRow,
  OriginShare,
  CostSums,
  CostSessionRow,
  UncalculableRow,
} from "../../bindings/sessionflow/internal/repository/sqlite";
export {
  BlockKind,
  ProviderID,
  Role,
} from "../../bindings/sessionflow/internal/domain";
export type {
  AppStatus,
  ProjectItem,
  ProviderStatus,
  SessionDetail,
  SessionItem,
  SessionPage,
  SessionQuery,
  TagItem,
  BlockView,
  MessageView,
  TranscriptPage,
  SearchHit,
  SearchQuery,
  SearchResult,
  SnippetPart,
  ResumePlan,
  SettingsView,
  ProviderSettings,
  TerminalSettings,
  StartupError,
  Dashboard,
  StatsQuery,
  SeriesPoint,
  SessionTimeline,
  TimelinePoint,
  Metric,
  ProviderShare,
  RecentTabs,
  AnalyticsQuery,
  Overview,
  UsageView,
  UsageSessionPage,
  ActivityView,
  WindowInfo,
  Availability,
  UsagePoint,
  CostView,
  CostGroup,
  CostPoint,
  CostSessionPage,
  UncalculablePage,
  RecalcPreview,
  PriceInput,
  PriceItem,
  PatternsView,
  Distribution,
  DayCount,
  ProjectActivity,
  ProjectProviders,
  ProviderPattern,
} from "../../bindings/sessionflow/internal/service";
export type { PriceRevision } from "../../bindings/sessionflow/internal/domain";

import type {
  AppStatus,
  SearchQuery,
  SessionQuery,
} from "../../bindings/sessionflow/internal/service";

// Slices Go nil chegam como null nos bindings; normaliza para lista vazia.
export function providersOf(status: AppStatus) {
  return status.providers ?? [];
}

export function list<T>(xs: T[] | null | undefined): T[] {
  return xs ?? [];
}

// Consulta completa a partir de campos parciais (o binding exige todos).
export function sessionQuery(q: Partial<SessionQuery>): SessionQuery {
  return {
    providers: [],
    project: "",
    favoritesOnly: false,
    tag: "",
    includeHidden: false,
    sort: "recent",
    limit: 50,
    offset: 0,
    ...q,
  };
}

// Busca completa a partir de campos parciais.
export function searchQuery(q: Partial<SearchQuery>): SearchQuery {
  return {
    text: "",
    providers: [],
    project: "",
    includeHidden: false,
    limit: 30,
    offset: 0,
    ...q,
  };
}

// Mensagem de erro de uma chamada ao backend.
export function errorMessage(err: unknown): string {
  const msg = err instanceof Error ? err.message : String(err ?? "");
  return msg || "Falha ao contatar o backend.";
}
