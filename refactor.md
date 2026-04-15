现在需要大改multica这个框架，大规模去除mutica agent cli的约束

不再以创建issues为单位，需要的架构是 每个runtime应该是一台服务器，然后要可以知道他们的GPU状态，里面可以分配projects给他们，分配之后会有调用一个harness agents 这个agent是需要真实和用户交互的，他先读projects的内容，然后启动Planner与用户进行多轮交互 (不要过multica的agent cli)，然后调度启动多个agents完成任务。参考/home/yuxuanhu/remote_code/phi-3-moe/streaming_s2st/.github中的harness mode v2的执行方式，创建多个agents。并且需要在图形界面监控这些agents的状态 是busy还是idle，然后可以点开每个agents，可以查阅他们的日志，输出的results.md，可历史对话。

runtime可以嵌套多个projects，projects中会有多个agents。skill和agents rule可以提前用于来写，用户写完projects之后可以图形界面分发到runtime。

所有的输出和临时文件都不要放到~或/tmp目录，而是放到/mnt2/yuxuanhu/multica/ 下 会有./<projects> ./<tmp> ./<skills> ./<agents>等文件