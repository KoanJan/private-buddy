/**
 * File type constants for document upload validation.
 * These formats are supported by the knowledge base text extraction pipeline.
 */

/**
 * Allowed file extensions for document upload.
 * Must match the backend constants in server/internal/constants/file_types.go
 */
export const ALLOWED_FILE_EXTENSIONS = ['.txt', '.md', '.pdf'] as const;

/**
 * File extension type for TypeScript type safety
 */
export type FileExtension = typeof ALLOWED_FILE_EXTENSIONS[number];

/**
 * Checks if a file extension is in the allowed list.
 * @param fileName - The file name to check
 * @returns true if the file extension is allowed, false otherwise
 */
export function isAllowedFileExtension(fileName: string): boolean {
  const lowerFileName = fileName.toLowerCase();
  return ALLOWED_FILE_EXTENSIONS.some(ext => lowerFileName.endsWith(ext));
}


