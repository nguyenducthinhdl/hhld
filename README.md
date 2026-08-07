# HHLD finance

**Constitution:** [spec/mission.md](spec/mission.md) · [spec/tech-stack.md](spec/tech-stack.md) · [spec/roadmap.md](spec/roadmap.md)

## Input from the owner
- This is the project aims to do a trading system to gain money efficiency and safety 
- There are serveral methods for trading, but the owner would like to trade firstly with some symbol like btcusd.
- The trading strategies:
+ Arbitrage: 1st method and we aim to do it for trading a symbol between two coin exchange once there are GAP from it. We need to care about the risk like latency, networking.
+ Prediction strategy: 2nd method, will be test after 1st method
- The functional feature of System:
 - There are some component of systems:
    - Trading Core: Core of trading where decide which volume, price of buy order and sell order. There are server submodules like: Connection Module, Risk Management Module, Testing Module, Strategy-Trading Module, Profit and Loss (PnL) Module. Every module must be written by interface for lose coupling and easy for testing and backtesting
    - BackTesting: Incharge to build a backtesting method for testing the Trading Core to verify the performance of the chosen trading strategy by simulating and get the profit and lost. Submodules: Simulation Module (Input is the trading data by tick and the strategy trading, output is PnL), Visualization Module (Visualize PnL time by time which helps trader to understand the Alpha)
    - Dataware House: A small dataware house to convert any crawling data to the Dataware House for Backtesting
    - Administrator: Contains auditable value: PnL, Storing Trading Orders
- The Non-functional:
    - High Performance: System should be fast and easy creating orders
    - Maintainable: Sourcode is readable
    - Monitoring and Tracing: Easy to trace the mismatching / losing order
    - Cost saving: In some first version, the infra deployment cost is small ( 1 medium instance )
    - Ready for Production of a 1 man company