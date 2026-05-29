import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { getFilesPaginated, getFilePublicLink, getThumbnailUrl, getSDCardFiles, getSDCardPreviewUrl, getSDCardFileUrl, getStatus } from '../api.js'
import { Card } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import { EmptyState } from '../components/EmptyState.jsx'
import { ErrorState } from '../components/ErrorState.jsx'

const DEFAULT_PAGE_SIZE = 40
const PAGE_SIZE_OPTIONS = [40, 80, 160, 320]
const VIEW_MODES = {
  compact: {
    label: 'Compact',
    icon: 'grid-small',
    grid: 'grid-cols-3 sm:grid-cols-5 md:grid-cols-7 lg:grid-cols-8',
  },
  comfortable: {
    label: 'Comfortable',
    icon: 'grid-medium',
    grid: 'grid-cols-2 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6',
  },
  large: {
    label: 'Large',
    icon: 'grid-large',
    grid: 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4',
  },
}
const VIEW_MODE_KEYS = Object.keys(VIEW_MODES)
const IMAGE_EXTENSIONS = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'tiff', 'tif', 'heic', 'heif', 'avif'])
const VIDEO_EXTENSIONS = new Set(['mp4', 'm4v', 'mov', 'avi', 'mkv', 'mts', 'm2ts', '3gp', 'webm'])
const RAW_EXTENSIONS = new Set(['arw', 'cr2', 'cr3', 'nef', 'nrw', 'dng', 'raf', 'rw2', 'orf', 'pef', 'srw', 'raw'])

function fileExtension(name) {
  const idx = name.lastIndexOf('.')
  return idx >= 0 ? name.slice(idx + 1).toLowerCase() : ''
}

function fileStem(name) {
  const idx = name.lastIndexOf('.')
  return idx >= 0 ? name.slice(0, idx) : name
}

function isImageFile(name) {
  return IMAGE_EXTENSIONS.has(fileExtension(name))
}

function isVideoFile(name) {
  return VIDEO_EXTENSIONS.has(fileExtension(name))
}

function isRawFile(name) {
  return RAW_EXTENSIONS.has(fileExtension(name))
}

// groupFileVariants merges sibling entries that share a basename (e.g.
// DSC0001.JPG + DSC0001.ARW) into a single group entry. The group keeps the
// image-typed variant as its preview/thumbnail source so we can still show a
// fast embedded-EXIF thumbnail for the JPG; RAW-only stems are returned as-is.
function groupFileVariants(files) {
  const groups = new Map()
  const out = []
  for (const file of files) {
    if (file.is_dir) {
      out.push(file)
      continue
    }
    const ext = fileExtension(file.name)
    const isImg = IMAGE_EXTENSIONS.has(ext) || file.is_image
    const isRaw = RAW_EXTENSIONS.has(ext) || file.is_raw
    if (!isImg && !isRaw) {
      out.push(file)
      continue
    }
    const stem = fileStem(file.name).toUpperCase()
    if (!groups.has(stem)) {
      const placeholder = { __placeholder: true, index: out.length, stem }
      groups.set(stem, { variants: [], placeholderIndex: out.push(placeholder) - 1 })
    }
    groups.get(stem).variants.push(file)
  }
  for (const { variants, placeholderIndex } of groups.values()) {
    if (variants.length === 1) {
      out[placeholderIndex] = variants[0]
      continue
    }
    const primary = variants.find((v) => IMAGE_EXTENSIONS.has(fileExtension(v.name)) || v.is_image) || variants[0]
    out[placeholderIndex] = {
      ...primary,
      name: primary.name,
      is_group: true,
      variants,
      size: variants.reduce((sum, v) => sum + (v.size || 0), 0),
    }
  }
  return out
}

function formatFileSize(bytes) {
  if (bytes === 0 || bytes == null) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let idx = 0
  let size = Math.abs(bytes)
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024
    idx++
  }
  return `${idx === 0 ? size : size.toFixed(1)} ${units[idx]}`
}

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

function buildPathSegments(currentPath) {
  if (!currentPath) return []
  return currentPath
    .split('/')
    .filter(Boolean)
    .map((segment, index, arr) => ({
      label: segment,
      path: arr.slice(0, index + 1).join('/'),
    }))
}

export default function GalleryPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()
  const reduceMotion = useReducedMotion() ?? false

  const [source, setSource] = useState('cloud')
  const [cloudPath, setCloudPath] = useState('')
  const [sdcardPath, setSdcardPath] = useState('')
  const [files, setFiles] = useState([])
  const [allSDCardFiles, setAllSDCardFiles] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [imagePreview, setImagePreview] = useState(null)
  const [videoPreview, setVideoPreview] = useState(null)
  const [variantPicker, setVariantPicker] = useState(null)
  const [showThumbnails, setShowThumbnails] = useState(false)
  const [viewMode, setViewMode] = useState(() => {
    if (typeof window === 'undefined') return 'comfortable'
    const stored = window.localStorage?.getItem('gallery:viewMode')
    return stored && VIEW_MODES[stored] ? stored : 'comfortable'
  })
  const [pageSize, setPageSize] = useState(() => {
    if (typeof window === 'undefined') return DEFAULT_PAGE_SIZE
    const stored = parseInt(window.localStorage?.getItem('gallery:pageSize') || '', 10)
    return PAGE_SIZE_OPTIONS.includes(stored) ? stored : DEFAULT_PAGE_SIZE
  })
  const [loadError, setLoadError] = useState(null)
  const requestIdRef = useRef(0)

  const currentPath = source === 'sdcard' ? sdcardPath : cloudPath
  const totalPages = useMemo(() => Math.max(1, Math.ceil(total / pageSize)), [total, pageSize])
  const segments = useMemo(() => buildPathSegments(currentPath), [currentPath])
  const thumbnailsAllowed = showThumbnails && source === 'sdcard'
  const gridClass = VIEW_MODES[viewMode]?.grid || VIEW_MODES.comfortable.grid

  const counts = useMemo(() => {
    let folders = 0
    let images = 0
    let videos = 0
    for (const f of files) {
      if (f.is_dir) folders++
      else if (source === 'sdcard' ? f.is_video || isVideoFile(f.name) : false) videos++
      else if (source === 'sdcard' ? f.is_image : isImageFile(f.name)) images++
    }
    return { folders, images, videos }
  }, [files, source])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage?.setItem('gallery:viewMode', viewMode)
    }
  }, [viewMode])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage?.setItem('gallery:pageSize', String(pageSize))
    }
  }, [pageSize])

  const setSourcePath = useCallback((nextPath) => {
    if (source === 'sdcard') setSdcardPath(nextPath)
    else setCloudPath(nextPath)
  }, [source])

  const fetchDeviceStatus = useCallback(async () => {
    if (!deviceUrl) return null
    try {
      return await getStatus(deviceUrl)
    } catch {
      return null
    }
  }, [deviceUrl])

  const fetchFiles = useCallback(async () => {
    if (!deviceUrl) return
    const requestId = requestIdRef.current + 1
    requestIdRef.current = requestId
    const isLatest = () => requestId === requestIdRef.current

    setLoading(true)
    setLoadError(null)
    try {
      const status = await fetchDeviceStatus()
      if (!isLatest()) return
      if (source === 'sdcard') {
        if (status && !status.sdcard_mounted) {
          setAllSDCardFiles([])
          setFiles([])
          setTotal(0)
          return
        }
        const data = await getSDCardFiles(deviceUrl, currentPath)
        if (!isLatest()) return
        if (data?.error) throw new Error(data.error)
        const fileArr = Array.isArray(data?.files) ? data.files : []
        fileArr.sort((a, b) => {
          if (a.is_dir && !b.is_dir) return -1
          if (!a.is_dir && b.is_dir) return 1
          return a.name.localeCompare(b.name)
        })
        const grouped = groupFileVariants(fileArr)
        setAllSDCardFiles(grouped)
        const start = (page - 1) * pageSize
        setFiles(grouped.slice(start, start + pageSize))
        setTotal(grouped.length)
      } else {
        const data = await getFilesPaginated(deviceUrl, {
          path: currentPath,
          page,
          pageSize,
        })
        if (!isLatest()) return
        if (data?.error) throw new Error(data.error)
        const fileArr = Array.isArray(data?.files) ? data.files : []
        fileArr.sort((a, b) => {
          if (a.is_dir && !b.is_dir) return -1
          if (!a.is_dir && b.is_dir) return 1
          return a.name.localeCompare(b.name)
        })
        setFiles(groupFileVariants(fileArr))
        setTotal(data?.total ?? fileArr.length)
      }
    } catch (err) {
      if (!isLatest()) return
      setLoadError(describeError(err))
      toast.error(`Could not load files: ${describeError(err)}`)
      setFiles([])
      setTotal(0)
    } finally {
      if (isLatest()) setLoading(false)
    }
  }, [deviceUrl, currentPath, page, pageSize, source, fetchDeviceStatus, toast])

  useEffect(() => {
    fetchFiles()
  }, [fetchFiles])

  const navigateTo = useCallback((path) => {
    setSourcePath(path)
    setPage(1)
    setFiles([])
    setLoadError(null)
  }, [setSourcePath])

  const handleSourceChange = useCallback((newSource) => {
    setSource(newSource)
    setPage(1)
    setFiles([])
    setLoadError(null)
  }, [])

  const handleFolderClick = useCallback(
    (folderPath) => {
      navigateTo(folderPath)
    },
    [navigateTo]
  )

  const getCloudFileUrl = useCallback(
    async (file) => {
      const data = await getFilePublicLink(deviceUrl, file.path)
      if (!data?.url) {
        throw new Error('Cloud did not return a file URL')
      }
      return data.url
    },
    [deviceUrl]
  )

  const handleImageClick = useCallback(
    async (file) => {
      if (!deviceUrl) return
      if (file.is_group && Array.isArray(file.variants) && file.variants.length > 1) {
        setVariantPicker(file)
        return
      }
      try {
        const url = source === 'sdcard'
          ? getSDCardPreviewUrl(deviceUrl, file.path)
          : await getCloudFileUrl(file)
        window.open(url, '_blank', 'noopener,noreferrer')
      } catch (err) {
        toast.error(`Could not open file: ${describeError(err)}`)
      }
    },
    [deviceUrl, getCloudFileUrl, source, toast]
  )

  const handleFilePreview = useCallback(
    async (file) => {
      if (!deviceUrl) return
      try {
        const url = source === 'sdcard'
          ? getSDCardPreviewUrl(deviceUrl, file.path)
          : await getCloudFileUrl(file)
        setImagePreview(url)
      } catch (err) {
        toast.error(`Could not preview file: ${describeError(err)}`)
      }
    },
    [deviceUrl, getCloudFileUrl, source, toast]
  )

  const handleFileDownload = useCallback(
    async (file) => {
      if (!deviceUrl) return
      if (file.is_group && Array.isArray(file.variants) && file.variants.length > 1) {
        setVariantPicker(file)
        return
      }
      try {
        const url = source === 'sdcard'
          ? getSDCardFileUrl(deviceUrl, file.path, { download: true })
          : await getCloudFileUrl(file)
        window.open(url, '_blank', 'noopener,noreferrer')
      } catch (err) {
        toast.error(`Could not download file: ${describeError(err)}`)
      }
    },
    [deviceUrl, getCloudFileUrl, source, toast]
  )

  const handleVideoPlay = useCallback(
    (file) => {
      if (!deviceUrl || source !== 'sdcard') return
      setVideoPreview({
        name: file.name,
        url: getSDCardFileUrl(deviceUrl, file.path),
      })
    },
    [deviceUrl, source]
  )

  const handleBreadcrumbClick = useCallback(
    (index) => {
      if (index === -1) {
        navigateTo('')
      } else {
        navigateTo(segments[index].path)
      }
    },
    [navigateTo, segments]
  )

  const handlePageChange = useCallback((newPage) => {
    setPage(newPage)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }, [])

  const goToPrev = useCallback(() => {
    if (page > 1) handlePageChange(page - 1)
  }, [page, handlePageChange])

  const goToNext = useCallback(() => {
    if (page < totalPages) handlePageChange(page + 1)
  }, [page, totalPages, handlePageChange])

  const visiblePages = useMemo(() => {
    const pages = []
    const start = Math.max(1, page - 2)
    const end = Math.min(totalPages, page + 2)
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }
    return pages
  }, [page, totalPages])

  // Close overlays on Escape for keyboard accessibility.
  useEffect(() => {
    if (!imagePreview && !videoPreview && !variantPicker) return
    const onKey = (e) => {
      if (e.key === 'Escape') {
        setImagePreview(null)
        setVideoPreview(null)
        setVariantPicker(null)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [imagePreview, videoPreview, variantPicker])

  const overlayDur = reduceMotion ? 0 : 0.18

  return (
    <div className="min-h-screen min-w-0 max-w-full overflow-x-hidden">
      {/* Toolbar */}
      <div className="mb-5 flex flex-col gap-3 rounded-xl border border-surface-700/50 bg-surface-800/40 p-3 backdrop-blur-sm sm:flex-row sm:items-center sm:justify-between">
        <div
          className="grid grid-cols-2 gap-0.5 rounded-lg border border-surface-700/60 bg-surface-900/60 p-1 sm:inline-flex"
          role="group"
          aria-label="File source"
        >
          {[
            { key: 'cloud', label: 'Cloud', icon: 'cloud' },
            { key: 'sdcard', label: 'SD Card', icon: 'sd-card' },
          ].map(({ key, label, icon }) => {
            const active = source === key
            return (
              <button
                key={key}
                onClick={() => handleSourceChange(key)}
                className={`relative flex items-center justify-center gap-1.5 rounded-md px-3.5 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 focus-visible:ring-offset-1 focus-visible:ring-offset-surface-900 ${
                  active ? 'text-white' : 'text-surface-400 hover:text-surface-200'
                }`}
                aria-pressed={active}
              >
                {active && (
                  <motion.span
                    layoutId="gallery-source-pill"
                    className="absolute inset-0 rounded-md bg-brand-600 shadow-lg shadow-brand-600/25"
                    transition={reduceMotion ? { duration: 0 } : { type: 'spring', stiffness: 500, damping: 36 }}
                  />
                )}
                <Icon name={icon} className="relative z-10 h-4 w-4" />
                <span className="relative z-10">{label}</span>
              </button>
            )
          })}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {source === 'sdcard' && (
            <button
              onClick={() => setShowThumbnails((enabled) => !enabled)}
              className={`flex min-h-10 items-center gap-1.5 rounded-md border px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 ${
                showThumbnails
                  ? 'border-brand-400/40 bg-brand-500/15 text-brand-200'
                  : 'border-surface-700/60 bg-surface-900/60 text-surface-300 hover:text-surface-100'
              }`}
              aria-pressed={showThumbnails}
            >
              <Icon name="image" className="h-4 w-4" />
              Thumbnails
            </button>
          )}
          <div
            className="flex rounded-md border border-surface-700/60 bg-surface-900/60 p-1"
            role="group"
            aria-label="Grid density"
          >
            {VIEW_MODE_KEYS.map((key) => {
              const { label, icon } = VIEW_MODES[key]
              const active = viewMode === key
              return (
                <button
                  key={key}
                  onClick={() => setViewMode(key)}
                  className={`relative flex h-8 w-8 items-center justify-center rounded transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 ${
                    active ? 'text-white' : 'text-surface-400 hover:text-surface-200'
                  }`}
                  aria-pressed={active}
                  aria-label={`${label} grid`}
                  title={label}
                >
                  {active && (
                    <motion.span
                      layoutId="gallery-density-pill"
                      className="absolute inset-0 rounded bg-brand-600 shadow shadow-brand-600/20"
                      transition={reduceMotion ? { duration: 0 } : { type: 'spring', stiffness: 500, damping: 36 }}
                    />
                  )}
                  <Icon name={icon} className="relative z-10 h-4 w-4" />
                </button>
              )
            })}
          </div>
          <label className="flex items-center gap-1.5 rounded-md border border-surface-700/60 bg-surface-900/60 px-2.5 py-1.5 text-xs text-surface-300">
            <span className="hidden sm:inline">Per page</span>
            <select
              value={pageSize}
              onChange={(e) => {
                const next = parseInt(e.target.value, 10)
                if (PAGE_SIZE_OPTIONS.includes(next)) {
                  setPageSize(next)
                  setPage(1)
                }
              }}
              className="cursor-pointer bg-transparent text-sm font-medium text-surface-100 focus:outline-none"
              aria-label="Items per page"
            >
              {PAGE_SIZE_OPTIONS.map((opt) => (
                <option key={opt} value={opt} className="bg-surface-900 text-surface-100">
                  {opt}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {/* Full-size image preview overlay */}
      <AnimatePresence>
        {imagePreview && (
          <motion.div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-4 backdrop-blur-md"
            onClick={() => setImagePreview(null)}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: overlayDur }}
            role="dialog"
            aria-modal="true"
            aria-label="Image preview"
          >
            <button
              className="absolute right-4 top-4 z-10 flex h-10 w-10 items-center justify-center rounded-full bg-white/10 text-white/80 backdrop-blur-sm transition hover:bg-white/20 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60"
              onClick={() => setImagePreview(null)}
              aria-label="Close preview"
            >
              <Icon name="x" className="h-6 w-6" />
            </button>
            <motion.img
              src={imagePreview}
              alt="Preview"
              className="max-h-[90vh] max-w-[92vw] rounded-xl object-contain shadow-2xl ring-1 ring-white/10"
              onClick={(e) => e.stopPropagation()}
              initial={reduceMotion ? false : { scale: 0.94, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={reduceMotion ? { opacity: 0 } : { scale: 0.96, opacity: 0 }}
              transition={{ type: 'spring', stiffness: 320, damping: 30 }}
            />
          </motion.div>
        )}
      </AnimatePresence>

      {/* Variant picker overlay */}
      <AnimatePresence>
        {variantPicker && (
          <VariantPicker
            group={variantPicker}
            reduceMotion={reduceMotion}
            onClose={() => setVariantPicker(null)}
            onPickImage={async (variant) => {
              setVariantPicker(null)
              try {
                const url = source === 'sdcard'
                  ? getSDCardPreviewUrl(deviceUrl, variant.path)
                  : await getCloudFileUrl(variant)
                window.open(url, '_blank', 'noopener,noreferrer')
              } catch (err) {
                toast.error(`Could not open file: ${describeError(err)}`)
              }
            }}
            onPickDownload={async (variant) => {
              setVariantPicker(null)
              try {
                const url = source === 'sdcard'
                  ? getSDCardFileUrl(deviceUrl, variant.path, { download: true })
                  : await getCloudFileUrl(variant)
                window.open(url, '_blank', 'noopener,noreferrer')
              } catch (err) {
                toast.error(`Could not download file: ${describeError(err)}`)
              }
            }}
          />
        )}
      </AnimatePresence>

      {/* Video preview overlay */}
      <AnimatePresence>
        {videoPreview && (
          <motion.div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 p-4 backdrop-blur-md"
            onClick={() => setVideoPreview(null)}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: overlayDur }}
            role="dialog"
            aria-modal="true"
            aria-label={`Playing ${videoPreview.name}`}
          >
            <button
              className="absolute right-4 top-4 z-10 flex h-10 w-10 items-center justify-center rounded-full bg-white/10 text-white/80 backdrop-blur-sm transition hover:bg-white/20 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60"
              onClick={() => setVideoPreview(null)}
              aria-label="Close video"
            >
              <Icon name="x" className="h-6 w-6" />
            </button>
            <motion.div
              className="w-full max-w-4xl"
              onClick={(e) => e.stopPropagation()}
              initial={reduceMotion ? false : { scale: 0.95, opacity: 0 }}
              animate={{ scale: 1, opacity: 1 }}
              exit={reduceMotion ? { opacity: 0 } : { scale: 0.96, opacity: 0 }}
              transition={{ type: 'spring', stiffness: 320, damping: 30 }}
            >
              <video
                src={videoPreview.url}
                controls
                autoPlay
                className="max-h-[85vh] w-full rounded-xl bg-black shadow-2xl ring-1 ring-white/10"
              />
              <p className="mt-3 truncate text-center text-sm text-white/70">{videoPreview.name}</p>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Breadcrumb navigation */}
      <nav className="mb-5" aria-label="Breadcrumb">
        <div className="flex flex-wrap items-center gap-1 rounded-lg border border-surface-700/40 bg-surface-900/40 px-2 py-1.5">
          <button
            onClick={() => handleBreadcrumbClick(-1)}
            className="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm font-medium text-surface-50 transition-colors hover:bg-surface-700/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 active:scale-[0.97]"
          >
            <Icon name="home" className="h-4 w-4 text-brand-400" />
            <span className="hidden sm:inline">Home</span>
          </button>
          {segments.map((seg, idx) => (
            <div key={seg.path} className="flex items-center gap-1">
              <Icon name="chevron-right" className="h-3.5 w-3.5 flex-shrink-0 text-surface-600" />
              <button
                onClick={() => handleBreadcrumbClick(idx)}
                className={`rounded-md px-2 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 active:scale-[0.97] ${
                  idx === segments.length - 1
                    ? 'cursor-default bg-brand-400/10 text-brand-300'
                    : 'text-surface-300 hover:bg-surface-700/50 hover:text-surface-100'
                }`}
                aria-current={idx === segments.length - 1 ? 'page' : undefined}
              >
                <span className="inline-block max-w-[120px] truncate sm:max-w-[200px]">
                  {seg.label}
                </span>
              </button>
            </div>
          ))}
        </div>
      </nav>

      {/* File listing */}
      {loading ? (
        <GallerySkeleton gridClass={gridClass} count={Math.min(pageSize, 18)} />
      ) : loadError ? (
        <ErrorState
          error={loadError}
          onRetry={fetchFiles}
          title="Could not load files"
        />
      ) : files.length === 0 ? (
        <Card className="py-12">
          <EmptyState
            icon={source === 'sdcard' ? 'sd-card' : 'folder'}
            title={source === 'sdcard' ? 'No SD Card Detected' : 'This folder is empty'}
            description={
              source === 'sdcard'
                ? 'Insert an SD card to view its contents. Make sure the card is properly seated in the slot.'
                : 'This folder does not contain any files or subfolders yet.'
            }
          />
        </Card>
      ) : (
        <>
          {/* Summary bar */}
          <div className="mb-3 flex items-center justify-between gap-3 px-1 text-xs text-surface-500">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              {counts.folders > 0 && (
                <span className="flex items-center gap-1">
                  <Icon name="folder" className="h-3.5 w-3.5 text-brand-400/70" />
                  {counts.folders} {counts.folders === 1 ? 'folder' : 'folders'}
                </span>
              )}
              {counts.images > 0 && (
                <span className="flex items-center gap-1">
                  <Icon name="image" className="h-3.5 w-3.5 text-surface-500" />
                  {counts.images} {counts.images === 1 ? 'image' : 'images'}
                </span>
              )}
              {counts.videos > 0 && (
                <span className="flex items-center gap-1">
                  <Icon name="play" className="h-3.5 w-3.5 text-surface-500" />
                  {counts.videos} {counts.videos === 1 ? 'video' : 'videos'}
                </span>
              )}
            </div>
            <span className="tabular-nums">{total} {total === 1 ? 'item' : 'items'}</span>
          </div>

          {/* Grid layout */}
          <motion.div
            className={`grid gap-3 sm:gap-4 ${gridClass}`}
            initial={reduceMotion ? false : 'hidden'}
            animate="show"
            variants={{
              show: { transition: { staggerChildren: 0.015 } },
            }}
          >
            {files.map((file) => (
              <FileCard
                key={file.path}
                file={file}
                deviceUrl={deviceUrl}
                source={source}
                reduceMotion={reduceMotion}
                onFolderClick={handleFolderClick}
                onImageClick={handleImageClick}
                onImagePreview={handleFilePreview}
                onFileDownload={handleFileDownload}
                onVideoPlay={handleVideoPlay}
                showThumbnail={thumbnailsAllowed}
              />
            ))}
          </motion.div>

          {/* Pagination */}
          {totalPages > 1 && (
            <nav className="mt-8 flex flex-wrap items-center justify-center gap-1.5" aria-label="Pagination">
              <Button
                variant="ghost"
                size="sm"
                onClick={goToPrev}
                disabled={page <= 1}
                aria-label="Previous page"
              >
                <Icon name="chevron-left" className="h-4 w-4" />
                <span className="hidden sm:inline">Prev</span>
              </Button>

              {visiblePages[0] > 1 && (
                <>
                  <PaginationButton page={1} onClick={handlePageChange} />
                  {visiblePages[0] > 2 && (
                    <span className="px-1 text-sm text-surface-500">...</span>
                  )}
                </>
              )}

              {visiblePages.map((p) => (
                <PaginationButton
                  key={p}
                  page={p}
                  active={p === page}
                  onClick={handlePageChange}
                />
              ))}

              {visiblePages[visiblePages.length - 1] < totalPages && (
                <>
                  {visiblePages[visiblePages.length - 1] < totalPages - 1 && (
                    <span className="px-1 text-sm text-surface-500">...</span>
                  )}
                  <PaginationButton page={totalPages} onClick={handlePageChange} />
                </>
              )}

              <Button
                variant="ghost"
                size="sm"
                onClick={goToNext}
                disabled={page >= totalPages}
                aria-label="Next page"
              >
                <span className="hidden sm:inline">Next</span>
                <Icon name="chevron-right" className="h-4 w-4" />
              </Button>

              <span className="ml-3 hidden text-xs text-surface-500 sm:inline">
                Page {page} of {totalPages}
              </span>
            </nav>
          )}
        </>
      )}
    </div>
  )
}

function GallerySkeleton({ gridClass, count = 12 }) {
  return (
    <div className={`grid gap-3 sm:gap-4 ${gridClass}`} aria-hidden="true">
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="rounded-lg border border-surface-700/50 bg-surface-800/40 p-2.5"
        >
          <div className="mb-2.5 aspect-square animate-shimmer rounded-lg" />
          <div className="space-y-1.5">
            <div className="h-3 w-3/4 animate-shimmer rounded" />
            <div className="h-2 w-1/2 animate-shimmer rounded" />
          </div>
        </div>
      ))}
    </div>
  )
}

function PaginationButton({ page: pageNum, active = false, onClick }) {
  return (
    <button
      onClick={() => onClick(pageNum)}
      className={`flex h-9 min-w-[36px] items-center justify-center rounded-lg text-sm font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 active:scale-[0.97] ${
        active
          ? 'bg-brand-600 text-white shadow-lg shadow-brand-600/20'
          : 'text-surface-300 hover:bg-surface-700/50'
      }`}
      aria-current={active ? 'page' : undefined}
    >
      {pageNum}
    </button>
  )
}

function FileCard({ file, deviceUrl, source, reduceMotion, onFolderClick, onImageClick, onImagePreview, onFileDownload, onVideoPlay, showThumbnail }) {
  const [thumbLoaded, setThumbLoaded] = useState(false)
  const [thumbError, setThumbError] = useState(false)
  const isImg = !file.is_dir && (source === 'sdcard' ? file.is_image : isImageFile(file.name))
  const isVideo = !file.is_dir && source === 'sdcard' && (file.is_video || isVideoFile(file.name))
  const isRaw = !file.is_dir && !isImg && (source === 'sdcard' ? file.is_raw : isRawFile(file.name))

  const thumbUrl = getThumbnailUrl(deviceUrl, file.path)

  const variantLabels = useMemo(() => {
    if (!file.is_group || !Array.isArray(file.variants)) return null
    const labels = file.variants.map((v) => fileExtension(v.name).toUpperCase()).filter(Boolean)
    return Array.from(new Set(labels))
  }, [file])
  const displayName = file.is_group ? fileStem(file.name) : file.name

  const handleClick = () => {
    if (file.is_dir) {
      onFolderClick(file.path)
    } else if (isVideo) {
      onVideoPlay(file)
    } else if (isImg) {
      onImageClick(file)
    } else {
      onFileDownload(file)
    }
  }

  const handleKeyDown = (e) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      handleClick()
    }
  }

  return (
    <motion.div
      variants={
        reduceMotion
          ? undefined
          : { hidden: { opacity: 0, y: 8 }, show: { opacity: 1, y: 0, transition: { duration: 0.22 } } }
      }
      whileHover={reduceMotion ? undefined : { y: -3 }}
      transition={{ type: 'spring', stiffness: 400, damping: 26 }}
      className="min-w-0"
    >
      <div
        role="button"
        tabIndex={0}
        onClick={handleClick}
        onKeyDown={handleKeyDown}
        aria-label={file.is_dir ? `Open folder ${displayName}` : `Open ${displayName}`}
        className="group relative h-full cursor-pointer overflow-hidden rounded-xl border border-surface-700/60 bg-surface-800/55 p-2.5 shadow-sm shadow-black/10 transition-all duration-200 hover:border-brand-400/40 hover:shadow-lg hover:shadow-brand-400/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 focus-visible:ring-offset-2 focus-visible:ring-offset-surface-950"
      >
        {/* Thumbnail area */}
        <div className="relative mb-2.5 flex aspect-square items-center justify-center overflow-hidden rounded-lg bg-gradient-to-br from-surface-900/80 to-surface-950/60">
          {isImg && showThumbnail && !thumbError ? (
            <>
              {!thumbLoaded && <div className="absolute inset-0 animate-shimmer" />}
              <img
                src={thumbUrl}
                alt={file.name}
                loading="lazy"
                decoding="async"
                className={`h-full w-full object-cover transition-all duration-500 ease-out group-hover:scale-[1.04] ${
                  thumbLoaded ? 'scale-100 opacity-100 blur-0' : 'scale-105 opacity-0 blur-md'
                }`}
                onLoad={() => setThumbLoaded(true)}
                onError={() => setThumbError(true)}
              />
              <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/30 to-transparent opacity-0 transition-opacity duration-200 group-hover:opacity-100" />
            </>
          ) : file.is_dir ? (
            <div className="relative transition-transform duration-200 group-hover:scale-105">
              <Icon name="folder" className="h-10 w-10 text-brand-400/70 sm:h-12 sm:w-12" />
              <div className="absolute -bottom-1 -right-1 flex h-5 w-5 items-center justify-center rounded-full border border-surface-600 bg-surface-800">
                <Icon name="chevron-right" className="h-3 w-3 text-surface-400" />
              </div>
            </div>
          ) : isVideo ? (
            <div className="flex h-12 w-12 items-center justify-center rounded-full border border-surface-700 bg-surface-800/80 transition-transform duration-200 group-hover:scale-110">
              <Icon name="play" className="ml-0.5 h-6 w-6 text-brand-400" />
            </div>
          ) : isRaw ? (
            <div className="flex flex-col items-center gap-1 text-surface-500">
              <Icon name="image" className="h-9 w-9 sm:h-10 sm:w-10" />
              <span className="rounded bg-surface-700/60 px-1.5 py-0.5 text-[9px] font-semibold tracking-wide text-surface-300">
                RAW
              </span>
            </div>
          ) : (
            <Icon name="image" className="h-10 w-10 text-surface-600 sm:h-12 sm:w-12" />
          )}
        </div>

        {/* File info */}
        <div className="space-y-0.5">
          <p
            className="truncate text-xs font-medium text-surface-100 sm:text-sm"
            title={file.name}
          >
            {displayName}
          </p>
          <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[10px] text-surface-400 sm:text-xs">
            {variantLabels && variantLabels.length > 0 && (
              <span className="rounded bg-brand-400/15 px-1.5 py-0.5 font-semibold text-brand-300">
                {variantLabels.join(' · ')}
              </span>
            )}
            {!file.is_dir && file.size != null && (
              <span className="tabular-nums">{formatFileSize(file.size)}</span>
            )}
            {!file.is_dir && file.size != null && file.mod_time && (
              <span className="text-surface-600">&middot;</span>
            )}
            {file.mod_time && <span>{formatDate(file.mod_time)}</span>}
          </div>
        </div>

        {/* Image preview button overlay */}
        {isImg && (
          <button
            className="absolute right-2 top-2 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white/80 opacity-0 backdrop-blur-sm transition hover:text-white focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70 group-hover:opacity-100"
            onClick={(e) => {
              e.stopPropagation()
              onImagePreview(file)
            }}
            aria-label={`Preview ${file.name}`}
          >
            <Icon name="magnifying" className="h-3.5 w-3.5" />
          </button>
        )}

        {/* Video play button overlay */}
        {isVideo && (
          <button
            className="absolute right-2 top-2 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white/80 opacity-100 backdrop-blur-sm transition hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70 sm:opacity-0 sm:group-hover:opacity-100"
            onClick={(e) => {
              e.stopPropagation()
              onVideoPlay(file)
            }}
            aria-label={`Play ${file.name}`}
          >
            <Icon name="play" className="ml-0.5 h-3.5 w-3.5" />
          </button>
        )}

        {/* Download button for files */}
        {!file.is_dir && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              onFileDownload(file)
            }}
            className="absolute bottom-2 right-2 flex h-7 w-7 items-center justify-center rounded-full bg-black/60 text-white/80 opacity-100 backdrop-blur-sm transition hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70 sm:opacity-0 sm:group-hover:opacity-100"
            aria-label={`Download ${file.name}`}
          >
            <Icon name="arrow-down-tray" className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
    </motion.div>
  )
}

function VariantPicker({ group, reduceMotion, onClose, onPickImage, onPickDownload }) {
  const variants = Array.isArray(group?.variants) ? group.variants : []
  return (
    <motion.div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4 backdrop-blur-md"
      onClick={onClose}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: reduceMotion ? 0 : 0.18 }}
      role="dialog"
      aria-modal="true"
      aria-label="Choose a format"
    >
      <motion.div
        className="w-full max-w-sm overflow-hidden rounded-xl border border-surface-700 bg-surface-900 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
        initial={reduceMotion ? false : { scale: 0.95, opacity: 0, y: 8 }}
        animate={{ scale: 1, opacity: 1, y: 0 }}
        exit={reduceMotion ? { opacity: 0 } : { scale: 0.96, opacity: 0 }}
        transition={{ type: 'spring', stiffness: 340, damping: 30 }}
      >
        <div className="flex items-center justify-between border-b border-surface-700 px-4 py-3">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-surface-100">{fileStem(group.name)}</p>
            <p className="text-xs text-surface-400">{variants.length} formats available</p>
          </div>
          <button
            className="rounded-md p-1 text-surface-300 transition hover:bg-surface-800 hover:text-surface-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
            onClick={onClose}
            aria-label="Close"
          >
            <Icon name="x" className="h-5 w-5" />
          </button>
        </div>
        <ul className="divide-y divide-surface-800">
          {variants.map((variant) => {
            const ext = fileExtension(variant.name).toUpperCase()
            const previewable = isImageFile(variant.name) || variant.is_image
            return (
              <li key={variant.path} className="flex items-center gap-3 px-4 py-3">
                <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-surface-700 bg-surface-800/80 text-[10px] font-bold text-brand-300">
                  {ext.slice(0, 4) || 'FILE'}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-semibold text-surface-100">{ext || 'FILE'}</p>
                  <p className="truncate text-xs text-surface-400">
                    {variant.name}
                    {variant.size != null && ` · ${formatFileSize(variant.size)}`}
                  </p>
                </div>
                {previewable ? (
                  <Button size="sm" onClick={() => onPickImage(variant)}>
                    <Icon name="magnifying" className="h-4 w-4" />
                    Open
                  </Button>
                ) : (
                  <Button size="sm" variant="secondary" onClick={() => onPickDownload(variant)}>
                    <Icon name="arrow-down-tray" className="h-4 w-4" />
                    Download
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      </motion.div>
    </motion.div>
  )
}
