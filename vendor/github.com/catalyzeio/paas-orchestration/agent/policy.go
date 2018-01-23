package agent

type Policy interface {
	Utilization() (float64, float64)
	Active(jobInfo *JobInfo)
	Inactive(jobInfo *JobInfo)
	Bid(jobID string, description *JobDescription, affinity *JobAffinity, replacedJob *JobInfo) *float64
}

type FixedPolicy struct {
	Used     int
	Capacity int

	pack bool
}

func NewFixedPolicy(capacity int, pack bool) *FixedPolicy {
	log.Info("Available capacity: %d slots", capacity)
	return &FixedPolicy{0, capacity, pack}
}

func (p *FixedPolicy) Utilization() (float64, float64) {
	return float64(p.Used), float64(p.Capacity)
}

func (p *FixedPolicy) Active(jobInfo *JobInfo) {
	p.Used++
	log.Info("Job %s active; usage count is now %d", jobInfo.ID, p.Used)
}

func (p *FixedPolicy) Inactive(jobInfo *JobInfo) {
	p.Used--
	log.Info("Job %s inactive; usage count is now %d", jobInfo.ID, p.Used)
}

func (p *FixedPolicy) Bid(jobID string, description *JobDescription, affinity *JobAffinity, replacedJob *JobInfo) *float64 {
	used, capacity := p.Used, p.Capacity
	if log.IsDebugEnabled() {
		log.Debug("Computing bid for job %s: affinity=%+v, used=%d, capacity=%d",
			jobID, affinity, used, capacity)
	}
	if replacedJob != nil {
		used--
	}
	// check basic capacity
	if capacity != 0 && used >= capacity {
		if log.IsDebugEnabled() {
			log.Debug("No more slots to run job %s", jobID)
		}
		return nil
	}
	// adjust used count based on affinity settings
	adjusted := float64(used)
	measure := affinity.PrefersCount - affinity.DespisesCount
	if measure > 0 {
		// more preferred services; adjust down
		adjusted /= 8.0
	} else if measure < 0 {
		// more despised services; adjust up
		adjusted *= 8.0
	}
	// ensure adjusted count is not negative
	if adjusted < 0.0 {
		adjusted = 0.0
	}
	// bid based on remaining capacity
	if capacity > 0 {
		bid := adjusted / float64(capacity)
		if !p.pack {
			bid = 1.0 - bid
		}
		return &bid
	}
	// No capacity set; compute bid based on number of resident jobs.
	// The "pack" setting is ignored here to prevent hosts from running away with all jobs.
	bid := 1.0 / (adjusted + 1.0)
	return &bid
}

type MemoryPolicy struct {
	Used     int64
	Capacity int64

	pack    bool
	minSize int64
	maxSize int64
}

const (
	defaultMemoryValue = 1024
)

func NewMemoryPolicy(capacity int64, limit float64, pack bool, minSize, maxSize int64) *MemoryPolicy {
	if capacity <= 0 {
		capacity = availableMemoryMiB()
	}
	capacity = int64(float64(capacity) * limit)
	log.Info("Available memory capacity: %d MiB", capacity)
	return &MemoryPolicy{0, capacity, pack, minSize, maxSize}
}

func (p *MemoryPolicy) Utilization() (float64, float64) {
	return float64(p.Used), float64(p.Capacity)
}

func (p *MemoryPolicy) Active(jobInfo *JobInfo) {
	p.Used += memoryFootprintMiB(jobInfo.ID, jobInfo.Description)
	log.Info("Job %s active; memory usage is now %d MiB", jobInfo.ID, p.Used)
}

func (p *MemoryPolicy) Inactive(jobInfo *JobInfo) {
	p.Used -= memoryFootprintMiB(jobInfo.ID, jobInfo.Description)
	log.Info("Job %s inactive; memory usage is now %d MiB", jobInfo.ID, p.Used)
}

func (p *MemoryPolicy) Bid(jobID string, description *JobDescription, affinity *JobAffinity, replacedJob *JobInfo) *float64 {
	size := memoryFootprintMiB(jobID, description)
	used, capacity := p.Used, p.Capacity
	if log.IsDebugEnabled() {
		log.Debug("Computing bid for job %s: affinity=%+v, used=%d, capacity=%d, size=%d",
			jobID, affinity, used, capacity, size)
	}
	if replacedJob != nil {
		used -= memoryFootprintMiB(replacedJob.ID, replacedJob.Description)
	}
	// check min/max size settings
	if size < p.minSize {
		if log.IsDebugEnabled() {
			log.Debug("Job %s is too small to run on this host (min=%d)", jobID, p.minSize)
		}
		return nil
	}
	if p.maxSize > 0 && size > p.maxSize {
		if log.IsDebugEnabled() {
			log.Debug("Job %s is too large to run on this host (max=%d)", jobID, p.maxSize)
		}
		return nil
	}
	// check basic capacity
	used += size
	if used > capacity {
		if log.IsDebugEnabled() {
			log.Debug("Not enough memory to run job %s", jobID)
		}
		return nil
	}
	// adjust used count based on affinity settings
	adjusted := float64(used)
	measure := affinity.PrefersCount - affinity.DespisesCount
	if measure > 0 {
		// more preferred services; adjust down
		adjusted /= 2.0
	} else if measure < 0 {
		// more despised services; adjust up
		adjusted *= 2.0
	}
	// ensure adjusted count is not negative
	if adjusted < 0.0 {
		adjusted = 0.0
	}
	// bid based on remaining capacity
	bid := adjusted / float64(capacity)
	if !p.pack {
		bid = 1.0 - bid
	}
	return &bid
}

func memoryFootprintMiB(jobID string, description *JobDescription) int64 {
	if description == nil {
		log.Warn("Job %s missing description; falling back to canned value", jobID)
		return defaultMemoryValue
	}
	resources := description.Resources
	if resources == nil {
		log.Warn("Job %s missing resources data; falling back to canned value", jobID)
		return defaultMemoryValue
	}
	memory := resources.Memory
	if memory <= 0 {
		log.Warn("Job %s missing memory size; falling back to canned value", jobID)
		return defaultMemoryValue
	}
	return memory / 1024 / 1024
}
