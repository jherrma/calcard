export interface ImportError {
  index: number;
  uid?: string;
  summary?: string;
  error: string;
}

export interface ImportResult {
  total: number;
  imported: number;
  skipped: number;
  failed: number;
  errors?: ImportError[];
}
