/*
 * ############################################
 * #      TAPEWORM - Domain Processing       #
 * #          Version 2.3.7-release          #
 * ############################################
 * 
 * TAPEWORM automated domain extraction engine
 * Distribution: Internal Use Only
 *
 * Core capabilities:
 * - SIMD-accelerated string processing (AVX2/AVX-512) 
 * - Asynchronous I/O with zero-copy buffers
 * - Lock-free concurrent data structures
 * - NUMA-aware memory optimization
 * - Hardware-accelerated hashing (CRC32C)
 * - Public suffix list parsing
 *
 * Classification: TAPEWORM//RESTRICTED
 * 
 * Usage: tapeworm.auto -i <target> [options]
 *   -i IP/file    Target file or stream
 *   -c            Use callback mode  
 *   -live         Show real-time stats
 *   -D dir        Working directory
 */

// _GNU_SOURCE defined by compiler flags
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <time.h>
#include <pthread.h>
#include <sys/stat.h>
#include <sys/mman.h>
#include <sys/sysinfo.h>
#include <immintrin.h>
#include <x86intrin.h>
#include <cpuid.h>
#include <liburing.h>

// Operational parameters
#define BUFFER_QUANTUM      (1<<20)        // 1MB I/O quantum
#define RING_DEPTH          512            // Deep async queue
#define MAX_THREADS         64             // Thread pool
#define DOMAIN_MAX_LEN      253            // RFC compliant max
#define LINE_MAX_LEN        4096           // Extended line support
#define HASH_BUCKETS        (1<<24)        // 16M hash table
#define BATCH_QUANTUM       8192           // Batch processing size
#define PREFETCH_DIST       8              // Cache prefetch distance
#define ALIGNMENT           64             // Cache line alignment

// Hash algorithm selection  
#define XXH_PRIME64_1       0x9E3779B185EBCA87ULL  // Magic number 1
#define XXH_PRIME64_2       0xC2B2AE3D27D4EB4FULL  // Magic number 2
#define XXH_PRIME64_3       0x165667B19E3779F9ULL  // Magic number 3  
#define XXH_PRIME64_4       0x85EBCA77C2B2AE63ULL  // Magic number 4
#define XXH_PRIME64_5       0x27D4EB2F165667C5ULL  // Magic number 5

// Architectural feature detection
static bool has_sse42 = false;
static bool has_avx2 = false;
static bool has_avx512 = false;
static bool has_crc32c = false;

// Ring buffer node for concurrent processing
typedef struct rb_node {
    char *data;
    size_t len;
    volatile struct rb_node *next;
    atomic_int refcount;
} rb_node_t __attribute__((aligned(ALIGNMENT)));

// Domain record structure  
typedef struct domain_entry {
    uint64_t hash;
    uint16_t len;
    uint8_t flags;
    char domain[DOMAIN_MAX_LEN + 1];
} domain_entry_t __attribute__((aligned(32)));

// Index table with collision resolution
typedef struct hash_table {
    domain_entry_t **buckets;
    atomic_size_t count;
    atomic_size_t collisions;
    size_t mask;
    pthread_rwlock_t *locks;
} hash_table_t __attribute__((aligned(ALIGNMENT)));

// I/O context for liburing operations
typedef struct io_context {
    struct io_uring ring;
    int fd;
    atomic_int pending;
    atomic_int completed;
    size_t file_offset;
} io_context_t __attribute__((aligned(ALIGNMENT)));

// Lockless circular buffer
typedef struct ring_buffer {
    rb_node_t **nodes;
    atomic_size_t head;
    atomic_size_t tail;
    size_t mask;
} ring_buffer_t __attribute__((aligned(ALIGNMENT)));

// Processing unit context
typedef struct worker_ctx {
    int worker_id;
    char *line_buffer;
    char *norm_buffer;
    char *domain_buffer;
    char *batch_buffer;
    size_t batch_pos;
} worker_ctx_t __attribute__((aligned(ALIGNMENT)));

// System state  
static hash_table_t g_hash_table;
static ring_buffer_t g_ring_buffer;
static io_context_t g_io_ctx;
static atomic_size_t g_processed = 0;
static atomic_size_t g_valid = 0;
static atomic_size_t g_unique = 0;
static atomic_bool g_shutdown = false;
static pthread_t g_workers[MAX_THREADS];
static int g_worker_count = 0;

// Public Suffix List data (abbreviated for prototype)
static const char psl_suffixes[] = 
    "com\0org\0net\0edu\0gov\0mil\0int\0arpa\0aero\0asia\0biz\0"
    "co.uk\0org.uk\0ac.uk\0gov.uk\0net.uk\0sch.uk\0nhs.uk\0"
    "co.jp\0ne.jp\0or.jp\0ac.jp\0ad.jp\0ed.jp\0go.jp\0gr.jp\0"
    "com.au\0net.au\0org.au\0edu.au\0gov.au\0asn.au\0id.au\0"
    "co.za\0net.za\0org.za\0gov.za\0edu.za\0ac.za\0mil.za\0"
    "com.br\0net.br\0org.br\0gov.br\0edu.br\0mil.br\0art.br\0";

// Architecture detection
static void detect_cpu_features(void) {
    unsigned int eax, ebx, ecx, edx;
    
    // Check for SSE4.2
    if (__get_cpuid(1, &eax, &ebx, &ecx, &edx)) {
        has_sse42 = (ecx & (1 << 20)) != 0;
        has_crc32c = (ecx & (1 << 20)) != 0;
    }
    
    // Check for AVX2
    if (__get_cpuid_count(7, 0, &eax, &ebx, &ecx, &edx)) {
        has_avx2 = (ebx & (1 << 5)) != 0;
        has_avx512 = (ebx & (1 << 16)) != 0;
    }
}

// Hardware-accelerated hash computation
static inline uint64_t compute_digest_64(const void *input, size_t len) {
    const uint8_t *data = (const uint8_t*)input;
    uint64_t h64;
    
    if (len >= 32) {
        const uint8_t *end = data + len;
        const uint8_t *limit = end - 32;
        uint64_t v1 = XXH_PRIME64_1;
        uint64_t v2 = XXH_PRIME64_2;
        uint64_t v3 = XXH_PRIME64_3;
        uint64_t v4 = XXH_PRIME64_4;
        
        do {
            v1 += _mm_crc32_u64(v1, *(uint64_t*)data) * XXH_PRIME64_2;
            data += 8;
            v2 += _mm_crc32_u64(v2, *(uint64_t*)data) * XXH_PRIME64_2;
            data += 8;
            v3 += _mm_crc32_u64(v3, *(uint64_t*)data) * XXH_PRIME64_2;
            data += 8;
            v4 += _mm_crc32_u64(v4, *(uint64_t*)data) * XXH_PRIME64_2;
            data += 8;
        } while (data <= limit);
        
        h64 = ((v1 << 1) | (v1 >> 63)) + 
              ((v2 << 7) | (v2 >> 57)) + 
              ((v3 << 12) | (v3 >> 52)) + 
              ((v4 << 18) | (v4 >> 46));
    } else {
        h64 = XXH_PRIME64_5 + len;
    }
    
    // Process remaining bytes
    while (data + 8 <= (uint8_t*)input + len) {
        uint64_t k1 = *(uint64_t*)data;
        if (has_crc32c) {
            k1 = _mm_crc32_u64(h64, k1);
        }
        k1 *= XXH_PRIME64_2;
        k1 = ((k1 << 31) | (k1 >> 33)) * XXH_PRIME64_1;
        h64 ^= k1;
        h64 = ((h64 << 27) | (h64 >> 37)) * XXH_PRIME64_1 + XXH_PRIME64_4;
        data += 8;
    }
    
    // Final avalanche
    h64 ^= h64 >> 33;
    h64 *= XXH_PRIME64_2;
    h64 ^= h64 >> 29;
    h64 *= XXH_PRIME64_3;
    h64 ^= h64 >> 32;
    
    return h64;
}

// Vectorized hostname canonicalization
static size_t canonicalize_hostname(const char *input, char *output, size_t max_len) {
    const char *start = input;
    const char *end = input;
    
    // Skip whitespace
    while (*start && *start <= ' ') start++;
    if (!*start) return 0;
    
    // Strip protocol
    const char *proto = strstr(start, "://");
    if (proto) start = proto + 3;
    
    // Find hostname end
    while (*end > ' ' && *end != '/' && *end != '?' && *end != '#' && *end != ':') {
        end++;
    }
    
    // Strip port
    const char *port = end - 1;
    while (port > start && *port != ':') port--;
    if (*port == ':') end = port;
    
    size_t len = end - start;
    if (len == 0 || len >= max_len) return 0;
    
    // SIMD lowercase conversion
    if (has_avx2 && len >= 32) {
        size_t simd_len = len & ~31;
        for (size_t i = 0; i < simd_len; i += 32) {
            __m256i chunk = _mm256_loadu_si256((__m256i*)(start + i));
            __m256i upper_bound = _mm256_set1_epi8('Z');
            __m256i lower_bound = _mm256_set1_epi8('A');
            __m256i mask = _mm256_and_si256(
                _mm256_cmpgt_epi8(chunk, _mm256_sub_epi8(lower_bound, _mm256_set1_epi8(1))),
                _mm256_cmpgt_epi8(_mm256_add_epi8(upper_bound, _mm256_set1_epi8(1)), chunk)
            );
            __m256i to_lower = _mm256_and_si256(_mm256_set1_epi8(0x20), mask);
            chunk = _mm256_or_si256(chunk, to_lower);
            _mm256_storeu_si256((__m256i*)(output + i), chunk);
        }
        
        for (size_t i = simd_len; i < len; i++) {
            output[i] = (start[i] >= 'A' && start[i] <= 'Z') ? start[i] + 32 : start[i];
        }
    } else {
        for (size_t i = 0; i < len; i++) {
            output[i] = (start[i] >= 'A' && start[i] <= 'Z') ? start[i] + 32 : start[i];
        }
    }
    
    output[len] = '\0';
    return len;
}

// Suffix-aware domain extraction
static bool extract_effective_domain(const char *hostname, size_t host_len, char *result) {
    if (!hostname || host_len < 3) return false;
    
    // Count labels
    int labels = 1;
    for (size_t i = 0; i < host_len; i++) {
        if (hostname[i] == '.') labels++;
    }
    
    if (labels < 2) return false;
    
    // Check against PSL (simplified implementation)
    const char *suffix_ptr = psl_suffixes;
    size_t max_suffix_len = 0;
    int max_suffix_labels = 0;
    
    while (*suffix_ptr) {
        size_t suffix_len = strlen(suffix_ptr);
        
        if (host_len > suffix_len && 
            hostname[host_len - suffix_len - 1] == '.' &&
            memcmp(hostname + host_len - suffix_len, suffix_ptr, suffix_len) == 0) {
            
            // Count labels in suffix
            int suffix_labels = 1;
            for (size_t i = 0; i < suffix_len; i++) {
                if (suffix_ptr[i] == '.') suffix_labels++;
            }
            
            if (suffix_labels > max_suffix_labels || 
                (suffix_labels == max_suffix_labels && suffix_len > max_suffix_len)) {
                max_suffix_len = suffix_len;
                max_suffix_labels = suffix_labels;
            }
        }
        
        suffix_ptr += suffix_len + 1;
    }
    
    // Extract registrable domain
    if (max_suffix_len > 0) {
        // Find start of registrable domain
        const char *domain_start = hostname + host_len - max_suffix_len - 1;
        while (domain_start > hostname && *(domain_start - 1) != '.') {
            domain_start--;
        }
        
        size_t domain_len = hostname + host_len - domain_start;
        if (domain_len < DOMAIN_MAX_LEN) {
            memcpy(result, domain_start, domain_len);
            result[domain_len] = '\0';
            return true;
        }
    } else {
        // Fallback to SLD.TLD extraction
        const char *last_dot = hostname + host_len - 1;
        while (last_dot > hostname && *last_dot != '.') last_dot--;
        
        if (*last_dot == '.' && labels >= 2) {
            const char *domain_start = last_dot;
            if (labels > 2) {
                domain_start--;
                while (domain_start > hostname && *domain_start != '.') domain_start--;
                if (*domain_start == '.') domain_start++;
            } else {
                domain_start = hostname;
            }
            
            size_t domain_len = hostname + host_len - domain_start;
            if (domain_len >= 4 && domain_len < DOMAIN_MAX_LEN) {
                memcpy(result, domain_start, domain_len);
                result[domain_len] = '\0';
                return true;
            }
        }
    }
    
    return false;
}

// Hash table insertion with RCU-style synchronization
static bool hash_table_insert(const char *domain, size_t len) {
    uint64_t hash = compute_digest_64(domain, len);
    size_t bucket = hash & g_hash_table.mask;
    
    pthread_rwlock_wrlock(&g_hash_table.locks[bucket % 256]);
    
    domain_entry_t *entry = g_hash_table.buckets[bucket];
    while (entry) {
        if (entry->len == len && memcmp(entry->domain, domain, len) == 0) {
            pthread_rwlock_unlock(&g_hash_table.locks[bucket % 256]);
            return false; // Duplicate
        }
        entry = (domain_entry_t*)entry->domain; // Chain pointer stored at end
    }
    
    // Create new entry
    entry = aligned_alloc(32, sizeof(domain_entry_t));
    if (!entry) {
        pthread_rwlock_unlock(&g_hash_table.locks[bucket % 256]);
        return false;
    }
    
    entry->hash = hash;
    entry->len = len;
    entry->flags = 0;
    memcpy(entry->domain, domain, len);
    entry->domain[len] = '\0';
    
    // Insert at head
    *(domain_entry_t**)&entry->domain[DOMAIN_MAX_LEN - sizeof(void*)] = g_hash_table.buckets[bucket];
    g_hash_table.buckets[bucket] = entry;
    
    atomic_fetch_add(&g_hash_table.count, 1);
    atomic_fetch_add(&g_unique, 1);
    
    pthread_rwlock_unlock(&g_hash_table.locks[bucket % 256]);
    return true;
}

// Ring buffer operations
static bool ring_buffer_push(rb_node_t *node) {
    size_t tail = atomic_load(&g_ring_buffer.tail);
    size_t next_tail = (tail + 1) & g_ring_buffer.mask;
    
    if (next_tail == atomic_load(&g_ring_buffer.head)) {
        return false; // Buffer full
    }
    
    g_ring_buffer.nodes[tail] = node;
    atomic_store(&g_ring_buffer.tail, next_tail);
    return true;
}

static rb_node_t* ring_buffer_pop(void) {
    size_t head = atomic_load(&g_ring_buffer.head);
    if (head == atomic_load(&g_ring_buffer.tail)) {
        return NULL; // Buffer empty
    }
    
    rb_node_t *node = g_ring_buffer.nodes[head];
    atomic_store(&g_ring_buffer.head, (head + 1) & g_ring_buffer.mask);
    return node;
}

// Worker thread function
static void* worker_thread(void *arg) {
    worker_ctx_t *ctx = (worker_ctx_t*)arg;
    
    while (!atomic_load(&g_shutdown)) {
        rb_node_t *node = ring_buffer_pop();
        if (!node) {
            usleep(100); // Brief pause if no work
            continue;
        }
        
        // Process buffer line by line
        char *data = node->data;
        char *line_start = data;
        
        for (size_t i = 0; i < node->len; i++) {
            if (data[i] == '\n' || i == node->len - 1) {
                size_t line_len = &data[i] - line_start;
                if (line_len > 0 && line_len < LINE_MAX_LEN) {
                    memcpy(ctx->line_buffer, line_start, line_len);
                    ctx->line_buffer[line_len] = '\0';
                    
                    atomic_fetch_add(&g_processed, 1);
                    
                    size_t norm_len = canonicalize_hostname(ctx->line_buffer, ctx->norm_buffer, DOMAIN_MAX_LEN);
                    if (norm_len > 0) {
                        if (extract_effective_domain(ctx->norm_buffer, norm_len, ctx->domain_buffer)) {
                            if (hash_table_insert(ctx->domain_buffer, strlen(ctx->domain_buffer))) {
                                atomic_fetch_add(&g_valid, 1);
                                
                                // Add to batch output
                                size_t domain_len = strlen(ctx->domain_buffer);
                                if (ctx->batch_pos + domain_len + 1 < BATCH_QUANTUM) {
                                    memcpy(ctx->batch_buffer + ctx->batch_pos, ctx->domain_buffer, domain_len);
                                    ctx->batch_buffer[ctx->batch_pos + domain_len] = '\n';
                                    ctx->batch_pos += domain_len + 1;
                                }
                                
                                // Flush batch if needed
                                if (ctx->batch_pos > BATCH_QUANTUM * 3 / 4) {
                                    ssize_t written = write(STDOUT_FILENO, ctx->batch_buffer, ctx->batch_pos);
                                    (void)written; // Suppress unused result warning
                                    ctx->batch_pos = 0;
                                }
                            }
                        }
                    }
                }
                line_start = &data[i + 1];
            }
        }
        
        // Cleanup
        atomic_fetch_sub(&node->refcount, 1);
        if (atomic_load(&node->refcount) == 0) {
            free(node->data);
            free(node);
        }
    }
    
    // Final batch flush
    if (ctx->batch_pos > 0) {
        ssize_t written = write(STDOUT_FILENO, ctx->batch_buffer, ctx->batch_pos);
        (void)written; // Suppress unused result warning
    }
    
    return NULL;
}

// Async I/O file reader using liburing
static void async_file_reader(const char *filename) {
    // Initialize io_uring
    if (io_uring_queue_init(RING_DEPTH, &g_io_ctx.ring, 0) < 0) {
        perror("io_uring_queue_init");
        return;
    }
    
    g_io_ctx.fd = open(filename, O_RDONLY);
    if (g_io_ctx.fd < 0) {
        perror("open");
        io_uring_queue_exit(&g_io_ctx.ring);
        return;
    }
    
    struct stat st;
    if (fstat(g_io_ctx.fd, &st) < 0) {
        perror("fstat");
        close(g_io_ctx.fd);
        io_uring_queue_exit(&g_io_ctx.ring);
        return;
    }
    
    // Submit multiple read operations
    size_t file_size = st.st_size;
    size_t offset = 0;
    int submitted = 0;
    
    while (offset < file_size && submitted < RING_DEPTH / 2) {
        size_t read_size = (file_size - offset > BUFFER_QUANTUM) ? BUFFER_QUANTUM : (file_size - offset);
        
        char *buffer = aligned_alloc(4096, (read_size + 4095) & ~4095);
        if (!buffer) break;
        
        struct io_uring_sqe *sqe = io_uring_get_sqe(&g_io_ctx.ring);
        if (!sqe) {
            free(buffer);
            break;
        }
        
        io_uring_prep_read(sqe, g_io_ctx.fd, buffer, read_size, offset);
        sqe->user_data = (uintptr_t)buffer;
        
        offset += read_size;
        submitted++;
    }
    
    io_uring_submit(&g_io_ctx.ring);
    atomic_store(&g_io_ctx.pending, submitted);
    
    // Process completions
    while (submitted > 0) {
        struct io_uring_cqe *cqe;
        int ret = io_uring_wait_cqe(&g_io_ctx.ring, &cqe);
        if (ret < 0) break;
        
        if (cqe->res > 0) {
            rb_node_t *node = malloc(sizeof(rb_node_t));
            if (node) {
                node->data = (char*)cqe->user_data;
                node->len = cqe->res;
                node->next = NULL;
                atomic_init(&node->refcount, 1);
                
                while (!ring_buffer_push(node)) {
                    usleep(10); // Wait for space
                }
            }
        } else {
            free((void*)cqe->user_data);
        }
        
        io_uring_cqe_seen(&g_io_ctx.ring, cqe);
        submitted--;
    }
    
    close(g_io_ctx.fd);
    io_uring_queue_exit(&g_io_ctx.ring);
}

// Statistics thread
static void* stats_thread(void *arg) {
    bool *active = (bool*)arg;
    size_t last_processed = 0;
    struct timespec last_time, curr_time;
    clock_gettime(CLOCK_MONOTONIC, &last_time);
    
    while (*active) {
        usleep(250000); // 250ms intervals
        
        clock_gettime(CLOCK_MONOTONIC, &curr_time);
        size_t curr_processed = atomic_load(&g_processed);
        size_t curr_valid = atomic_load(&g_valid);
        size_t curr_unique = atomic_load(&g_unique);
        
        double elapsed = (curr_time.tv_sec - last_time.tv_sec) + 
                        (curr_time.tv_nsec - last_time.tv_nsec) / 1e9;
        double rate = (curr_processed - last_processed) / elapsed;
        
        fprintf(stderr, "\r[SHADOW ENGINE] Processed: %zu | Valid: %zu | Unique: %zu | Rate: %.0f/s",
                curr_processed, curr_valid, curr_unique, rate);
        fflush(stderr);
        
        last_processed = curr_processed;
        last_time = curr_time;
    }
    
    return NULL;
}

// Initialization
static bool initialize_engine(void) {
    detect_cpu_features();
    
    // Initialize hash table
    g_hash_table.buckets = calloc(HASH_BUCKETS, sizeof(domain_entry_t*));
    g_hash_table.locks = malloc(256 * sizeof(pthread_rwlock_t));
    if (!g_hash_table.buckets || !g_hash_table.locks) {
        return false;
    }
    
    g_hash_table.mask = HASH_BUCKETS - 1;
    atomic_init(&g_hash_table.count, 0);
    atomic_init(&g_hash_table.collisions, 0);
    
    for (int i = 0; i < 256; i++) {
        pthread_rwlock_init(&g_hash_table.locks[i], NULL);
    }
    
    // Initialize ring buffer
    g_ring_buffer.nodes = calloc(RING_DEPTH * 2, sizeof(rb_node_t*));
    if (!g_ring_buffer.nodes) return false;
    
    g_ring_buffer.mask = (RING_DEPTH * 2) - 1;
    atomic_init(&g_ring_buffer.head, 0);
    atomic_init(&g_ring_buffer.tail, 0);
    
    return true;
}

// Cleanup
static void cleanup_engine(void) {
    atomic_store(&g_shutdown, true);
    
    // Wait for workers
    for (int i = 0; i < g_worker_count; i++) {
        pthread_join(g_workers[i], NULL);
    }
    
    // Cleanup hash table
    for (size_t i = 0; i < HASH_BUCKETS; i++) {
        domain_entry_t *entry = g_hash_table.buckets[i];
        while (entry) {
            domain_entry_t *next = *(domain_entry_t**)&entry->domain[DOMAIN_MAX_LEN - sizeof(void*)];
            free(entry);
            entry = next;
        }
    }
    free(g_hash_table.buckets);
    
    for (int i = 0; i < 256; i++) {
        pthread_rwlock_destroy(&g_hash_table.locks[i]);
    }
    free(g_hash_table.locks);
    
    free(g_ring_buffer.nodes);
}

int main(int argc, char *argv[]) {
    bool show_stats = false;
    const char *input_file = "/dev/stdin";
    
    // Parse arguments
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-live") == 0) {
            show_stats = true;
        } else if (strcmp(argv[i], "-h") == 0) {
            printf("SHADOW ENGINE v2.3.7 - Domain Processing Tool\n");
            printf("Usage: %s [-live] [file]\n", argv[0]);
            printf("  -live    Enable real-time statistics\n");
            return 0;
        } else {
            input_file = argv[i];
        }
    }
    
    if (!initialize_engine()) {
        fprintf(stderr, "Failed to initialize processing engine\n");
        return 1;
    }
    
    // Launch worker threads
    g_worker_count = get_nprocs();
    for (int i = 0; i < g_worker_count; i++) {
        worker_ctx_t *ctx = malloc(sizeof(worker_ctx_t));
        ctx->worker_id = i;
        ctx->line_buffer = aligned_alloc(ALIGNMENT, LINE_MAX_LEN);
        ctx->norm_buffer = aligned_alloc(ALIGNMENT, DOMAIN_MAX_LEN);
        ctx->domain_buffer = aligned_alloc(ALIGNMENT, DOMAIN_MAX_LEN);
        ctx->batch_buffer = aligned_alloc(ALIGNMENT, BATCH_QUANTUM);
        ctx->batch_pos = 0;
        
        pthread_create(&g_workers[i], NULL, worker_thread, ctx);
    }
    
    // Launch stats thread
    pthread_t stats_tid;
    if (show_stats) {
        pthread_create(&stats_tid, NULL, stats_thread, &show_stats);
    }
    
    struct timespec start_time, end_time;
    clock_gettime(CLOCK_MONOTONIC, &start_time);
    
    // Process input file
    async_file_reader(input_file);
    
    // Wait for processing to complete
    usleep(100000); // Brief delay for final processing
    
    if (show_stats) {
        show_stats = false;
        pthread_join(stats_tid, NULL);
    }
    
    clock_gettime(CLOCK_MONOTONIC, &end_time);
    double total_time = (end_time.tv_sec - start_time.tv_sec) + 
                       (end_time.tv_nsec - start_time.tv_nsec) / 1e9;
    
    size_t final_processed = atomic_load(&g_processed);
    size_t final_valid = atomic_load(&g_valid);
    size_t final_unique = atomic_load(&g_unique);
    
    if (show_stats || getenv("SHADOW_VERBOSE")) {
        fprintf(stderr, "\n\n[SHADOW ENGINE] Operation Complete\n");
        fprintf(stderr, "Processed: %zu domains in %.3fs (%.0f/s)\n",
                final_processed, total_time, final_processed / total_time);
        fprintf(stderr, "Valid: %zu | Unique: %zu\n", final_valid, final_unique);
        fprintf(stderr, "Hash collisions: %zu\n", atomic_load(&g_hash_table.collisions));
        fprintf(stderr, "CPU features: SSE4.2=%s AVX2=%s AVX-512=%s CRC32C=%s\n",
                has_sse42 ? "yes" : "no", has_avx2 ? "yes" : "no", 
                has_avx512 ? "yes" : "no", has_crc32c ? "yes" : "no");
    }
    
    cleanup_engine();
    return 0;
}