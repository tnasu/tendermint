# RFC 016: Adding Signature Aggregation to Tendermint

## Changelog

- 01-April-2022: Initial draft (@williambanfield).

## Abstract

## Background

### What is an aggregated signature?

### What systems would be affected by adding aggregated signatures?

#### Gossip

#### Block Verification

## Discussion

### What are the proposed benefits to aggregated signatures?

#### Reduce Commit Size
* How big are commits now per validator? Well, it scales linearly with num vals
*
#### Reduce Gossip Bandwidth

#### Reduce Gossip Bandwidth

* Allow for smaller IBC Packets in Cosmos-> Tendermint headers will only require
* one signature Perform signature aggregation during gossip to reduce total
* bandwidth. Speed of signature verification

### What are the drawbacks to aggregated signatures?

#### Heterogeneous key types cannot be aggregated

### Can aggregated signatures be added as soft-upgrades?

### Implementing vote-time and block-time signature aggregation separately

#### Separable implementation

#### Simultaneous implementation

### References

[line-ostracton-repo]: https://github.com/line/ostracon [line-ostracton-pr]:
https://github.com/line/ostracon/pull/117 [mit-BLS-lecture]:
https://youtu.be/BFwc2XA8rSk?t=2521
